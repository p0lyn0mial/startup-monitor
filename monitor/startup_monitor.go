package monitor

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

// StartupMonitor is a controller that watches an operand's health condition
// and falls back to the previous version in case the current version is considered unhealthy.
//
// This controller understands a tree structure created by an OCP installation. That is:
//  The root manifest are looked up in the manifestPath
//  The revisioned manifest are looked up in the staticPodResourcesPath
//  The target (operand) name is derived from the targetName.
type StartupMonitor struct {
	// probeInterval specifies a time interval at which health of the target will be assessed
	// be mindful of not setting it too low, on each iteration, an i/o is involved
	probeInterval time.Duration

	// timeout specifies a timeout after which the monitor starts the fall back procedure
	timeout time.Duration

	// revision at which the monitor was started
	revision int

	// targetName hold the name of the operand
	// used to construct the final file name when reading the current and previous manifests
	targetName string

	// manifestsPath points to the directory that holds the root manifests
	manifestsPath string

	// staticPodResourcesPath points to the directory that holds revisioned manifests
	staticPodResourcesPath string

	// isTargetHealthy defines a function that abstracts away assessing operand's health condition.
	// the provided functions should be async and cheap in a sense that it shouldn't assess the target
	// only read the current state.
	// mainly because we acquire a lock on each sync.
	isTargetHealthy func() bool

	// records the time the monitor has started assessing operand's health condition
	monitorTimeStamp time.Time

	// io collects file system level operations that need to be mocked out during tests
	io ioInterface
}

func New(isTargetHealthy func() bool) *StartupMonitor {
	return &StartupMonitor{isTargetHealthy: isTargetHealthy, io: realFS{}}
}

func (sm *StartupMonitor) Run(ctx context.Context) {
	klog.Infof("Starting the startup monitor with Interval = %v, Timeout = %v", sm.probeInterval, sm.timeout)
	defer klog.Info("Shutting down the startup monitor")

	wait.Until(sm.syncErrorWrapper, sm.probeInterval, ctx.Done())
}

func (sm *StartupMonitor) syncErrorWrapper() {
	if err := sm.sync(); err != nil {
		klog.Error(err)
	}
}

func (sm *StartupMonitor) sync() error {
	//
	// TODO: acquire an exclusive lock to coordinate work with the installer pod
	//
	// a lock is required to protect the following case:
	//
	// an installer is in progress and wants to install a new revision
	// the current revision is not healthy and we are about to fall back to the previous version (fallbackToPreviousRevision method)
	// the installer writes the new file and we immediately overwrite it
	//
	// additional benefit is that we read consistent operand's manifest

	// to avoid issues on startup and downgrade (before the startup monitor was introduced check the current target's revision.
	// refrain from any further processing in case we have a mismatch.
	currentTargetRevision, err := sm.loadRootTargetPodAndExtractRevision()
	if err != nil {
		return err
	}
	if sm.revision != currentTargetRevision {
		klog.Info("Stopping further processing because the monitor is watching revision %d and the current target's revision is %d", sm.revision, currentTargetRevision)
		return nil
	}

	if sm.monitorTimeStamp.IsZero() {
		sm.monitorTimeStamp = time.Now()
	}

	// first check if the target is healthy
	// note that we will always reconcile on transient errors
	// before starting the fall back procedure
	if sm.isTargetHealthy() {
		klog.Info("Observed a healthy target, creating last known good revision")
		if err := sm.createLastKnowGoodRevisionAndDestroy(); err != nil {
			return err
		}
		return nil
	}

	// check if we reached the timeout
	if time.Now().After(sm.monitorTimeStamp.Add(sm.timeout)) {
		klog.Info("Timed out while waiting for the target to become healthy, starting a fall back procedure")
		if err := sm.fallbackToPreviousRevision(); err != nil {
			return err
		}
		return nil
	}

	return nil
}

func (sm *StartupMonitor) createLastKnowGoodRevisionAndDestroy() error {
	// step 0: rm the previous last good known revision if exists
	// step 1: create last known good revision
	if err := sm.createLastKnowGoodRevisionFor(sm.revision, true); err != nil {
		return err
	}

	// step 2: commit suicide
	return sm.io.Remove(path.Join(sm.manifestsPath, fmt.Sprintf("%s-startup-monitor.yaml", sm.targetName)))
}

// TODO: pruner|installer: protect the linked revision
func (sm *StartupMonitor) fallbackToPreviousRevision() error {
	// step 0: if the last known good revision doesn't exist
	//         find a previous revision to work with
	//         return in case no revision has been found
	//           TODO: or commit suicide as this seems to be fatal
	lastKnownExists, err := sm.fileExists(sm.lastKnownGoodManifestDstPath())
	if err != nil {
		return err
	}
	if !lastKnownExists {
		prevRev, found, err := sm.findPreviousRevision()
		if err != nil {
			return err
		}
		if !found {
			klog.Info("Unable to roll back because no previous revision hasn't been found for %s", sm.targetName)
			// TODO: commit suicide ? this seems to be fatal
			return nil
		}

		targetManifestForPrevRevExists, err := sm.fileExists(sm.targetManifestPathFor(prevRev))
		if err != nil {
			return err // retry, a transient err
		}
		if !targetManifestForPrevRevExists {
			klog.Info("Unable to roll back because a manifest %q hasn't been found for the previous revision %d", sm.targetManifestPathFor(prevRev), prevRev)
			// TODO: commit suicide ? this seems to be fatal
			return nil
		}

		// step 1: create the last known good revision file
		if err := sm.createLastKnowGoodRevisionFor(prevRev, false); err != nil {
			return err
		}
	}

	// step 2: if the last known good revision exits and we got here
	//         that could mean that:
	//          - the current revision is broken
	//          - we just created the last known good revision file
	//          - the previous iteration of the sync loop returned an error
	//
	//         in that case just:
	//          - annotate the manifest
	//          - copy the last known good revision manifest
	lastKnownGoodPod, err := sm.readTargetPod(sm.lastKnownGoodManifestDstPath())
	if err != nil {
		return err
	}
	if lastKnownGoodPod.Annotations == nil {
		lastKnownGoodPod.Annotations = map[string]string{}
	}
	lastKnownGoodPod.Annotations["startup-monitor.static-pods.openshift.io/fallback-for-revision"] = fmt.Sprintf("%d", sm.revision)

	// the kubelet has a bug that prevents graceful termination from working on static pods with the same name, filename
	// and uuid.  By setting the pod UID we can work around the kubelet bug and get our graceful termination honored.
	// Per the node team, this is hard to fix in the kubelet, though it will affect all static pods.
	lastKnownGoodPod.UID = uuid.NewUUID()

	// remove the existing file to ensure kubelet gets "create" event from inotify watchers
	rootTargetManifestPath := path.Join(sm.manifestsPath, fmt.Sprintf("%s-pod.yaml", sm.targetName))
	if err := sm.io.Remove(rootTargetManifestPath); err == nil {
		klog.Infof("Removed existing static pod manifest %q", path.Join(rootTargetManifestPath))
	} else if !os.IsNotExist(err) {
		return err
	}

	lastKnownGoodPodBytes := []byte(resourceread.WritePodV1OrDie(lastKnownGoodPod))
	klog.Infof("Writing a static pod manifest %q \n%s", path.Join(rootTargetManifestPath), lastKnownGoodPodBytes)
	if err := sm.io.WriteFile(path.Join(rootTargetManifestPath), lastKnownGoodPodBytes, 0644); err != nil {
		return err
	}

	// TODO: commit suicide ?
	return nil
}

func (sm *StartupMonitor) createLastKnowGoodRevisionFor(revision int, strict bool) error {
	var revisionedTargetManifestPath = sm.targetManifestPathFor(revision)

	// step 0: in strict mode remove the previous last good known revision if exists
	if strict {
		if exists, err := sm.fileExists(sm.lastKnownGoodManifestDstPath()); err != nil {
			return err
		} else if exists {
			if err := sm.io.Remove(sm.lastKnownGoodManifestDstPath()); err != nil {
				return err
			}
			klog.Info("Removed existing last known good revision manifest %s", sm.lastKnownGoodManifestDstPath())
		}
	}

	// step 1: create last known good revision
	if err := sm.io.Symlink(revisionedTargetManifestPath, sm.lastKnownGoodManifestDstPath()); err != nil {
		return fmt.Errorf("failed to create a symbolic link %q for %q due to %v", sm.lastKnownGoodManifestDstPath(), revisionedTargetManifestPath, err)
	}
	klog.Info("Created a symlink %s for %s", sm.lastKnownGoodManifestDstPath(), revisionedTargetManifestPath)
	return nil
}

// note that there is a fight between the installer pod (writer) and the startup monitor (reader) when dealing with the target manifest file.
// since the monitor is resynced every probeInterval it seems we can deal with an error or stale content
//
// note if this code will return buffered data due to perf reason revisit fallbackToPreviousRevision
// as it currently assumes strong consistency
func (sm *StartupMonitor) loadRootTargetPodAndExtractRevision() (int, error) {
	currentTargetPod, err := sm.readTargetPod(path.Join(sm.manifestsPath, fmt.Sprintf("%s-pod.yaml", sm.targetName)))
	if err != nil {
		return 0, err
	}

	revisionString, found := currentTargetPod.Labels["revision"]
	if !found {
		return 0, fmt.Errorf("pod %s doesn't have revision label", currentTargetPod.Name)
	}
	if len(revisionString) == 0 {
		return 0, fmt.Errorf("empty revision label on %s pod", currentTargetPod.Name)
	}
	revision, err := strconv.Atoi(revisionString)
	if err != nil || revision < 0 {
		return 0, fmt.Errorf("invalid revision label on pod %s: %q", currentTargetPod.Name, revisionString)
	}

	return revision, nil
}

func (sm *StartupMonitor) readTargetPod(filepath string) (*corev1.Pod, error) {
	rawManifest, err := sm.io.ReadFile(filepath)
	if err != nil {
		return nil, err
	}
	currentTargetPod, err := resourceread.ReadPodV1(rawManifest)
	if err != nil {
		return nil, err
	}
	return currentTargetPod, nil
}

func (sm *StartupMonitor) findPreviousRevision() (int, bool, error) {
	files, err := sm.io.ReadDir(sm.staticPodResourcesPath)
	if err != nil {
		return 0, false, err
	}

	var allRevisions []int
	for _, file := range files {
		// skip if the file is not a directory
		if !file.IsDir() {
			continue
		}

		// and doesn't match our prefix
		if !strings.HasPrefix(file.Name(), sm.targetName) {
			continue
		}

		klog.Infof("Considering %s for revision extraction", file.Name())
		// now split the file name to get just the revision
		fileSplit := strings.Split(file.Name(), sm.targetName+"-pod-")
		if len(fileSplit) != 2 {
			return 0, false, fmt.Errorf("unable to extract revision from %s due to incorrect format", file.Name())
		}
		revision, err := strconv.Atoi(fileSplit[1])
		if err != nil {
			return 0, false, err
		}
		allRevisions = append(allRevisions, revision)
	}

	if len(allRevisions) < 2 {
		return 0, false, nil
	}
	sort.IntSlice(allRevisions).Sort()
	return allRevisions[len(allRevisions)-2], true, nil
}

func (sm *StartupMonitor) fileExists(filepath string) (bool, error) {
	fileInfo, err := sm.io.Stat(filepath)
	if err == nil {
		if fileInfo.IsDir() {
			return false, fmt.Errorf("the provided path %v is incorrect and points to a directory", filepath)
		}
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}

	return false, nil
}

func (sm *StartupMonitor) lastKnownGoodManifestDstPath() string {
	return path.Join(sm.staticPodResourcesPath, fmt.Sprintf("%s-last-known-good", sm.targetName))
}

func (sm *StartupMonitor) targetManifestPathFor(revision int) string {
	return path.Join(sm.staticPodResourcesPath, fmt.Sprintf("%s-pod-%d", sm.targetName, revision), fmt.Sprintf("%s-pod.yaml", sm.targetName))
}
