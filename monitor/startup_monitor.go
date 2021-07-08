package monitor

import (
	"context"
	"fmt"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

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

	// staticPodResourcesPath points to the directory that holds revisioned pod manifests
	staticPodResourcesPath string

	// isTargetHealthy defines a function that abstracts away assessing operand's health condition
	isTargetHealthy func() bool

	// records the time the monitor has started assessing operand's health condition
	monitorTimeStamp time.Time

	// io collects file system level operations that need to be mocked out during tests
	io IOInterface
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
	// to avoid issues on startup and downgrade (before the startup monitor was introduced check the current target's revision.
	// refrain from any further processing in case we have a mismatch.
	currentTargetRevision, err := sm.loadTargetManifestAndExtractRevision()
	if err != nil {
		return err
	}
	if sm.revision != currentTargetRevision {
		klog.Info("stopping further processing because the monitor is watching revision %d and the current target's revision is %d", sm.revision, currentTargetRevision)
		return nil
	}

	if sm.monitorTimeStamp.IsZero() {
		sm.monitorTimeStamp = time.Now()
	}

	// check if we reach the timeout
	if time.Now().After(sm.monitorTimeStamp.Add(sm.timeout)) {
		klog.Info("timed out while waiting for the target to become healthy, starting a fall back procedure")
		if err := sm.fallbackToPreviousRevision(); err != nil {
			return err
		}
		return nil
	}

	// check if the target is healthy
	if sm.isTargetHealthy() {
		if err := sm.createLastKnowGoodRevisionAndExit(); err != nil {
			return err
		}
		return nil
	}

	return nil
}

func (sm *StartupMonitor) createLastKnowGoodRevisionAndExit() error {
	var lastKnowGoodManifestDstPath = path.Join(sm.staticPodResourcesPath, fmt.Sprintf("%s-last-know-good", sm.targetName))
	var lastKnowGoodManifestSrcPath = path.Join(sm.staticPodResourcesPath, fmt.Sprintf("%s-pod-%d", sm.targetName, sm.revision), fmt.Sprintf("%s-pod.yaml", sm.revision))

	// step 0: rm the previous last good known revision if exists
	fileInfo, err := sm.io.Stat(lastKnowGoodManifestDstPath)
	if err == nil {
		if fileInfo.IsDir() {
			return fmt.Errorf("the provided path %v is incorrect and points to a directory", lastKnowGoodManifestDstPath)
		}
		if err := sm.io.Remove(lastKnowGoodManifestDstPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	// step 1: create last know good revision
	if err := sm.io.Symlink(lastKnowGoodManifestDstPath, lastKnowGoodManifestSrcPath); err != nil {
		return fmt.Errorf("failed to create a symbolic link %q for %q due to %v", lastKnowGoodManifestDstPath, lastKnowGoodManifestSrcPath, err)
	}

	// step 2: commit suicide
	// TODO: ^
	return fmt.Errorf("implement me")
}

// TODO: pruner|installer: protect the lined revision
// TODO: step 4: commit suicide ?
func (sm *StartupMonitor) fallbackToPreviousRevision() error {
	// step 0: if the last know good revision doesn't exist
	//         find a previous revision to work with
	//         return in case no revision has been found

	// step 1: if last known good revision exits and we got here
	//         that could mean that:
	//          - the current revision is broken
	//          - the previous iteration of the sync loop returned an error
	//         in that case just proceed to the step 3

	// step 2: create the last known good revision file

	// step 3: copy the last known revision
	return fmt.Errorf("implement me")
}

// note that there is a fight between the installer pod (writer) and the startup monitor (reader) when dealing with the target manifest file.
// since the monitor is resynced every probeInterval it seems we can deal with an error or stale content
//
// note if this code will return buffered data due to perf reason revisit fallbackToPreviousRevision
// as it currently assumes strong consistency
func (sm *StartupMonitor) loadTargetManifestAndExtractRevision() (int, error) {
	rawManifest, err := sm.io.ReadFile(path.Join(sm.manifestsPath, fmt.Sprintf("%s-pod.yaml", sm.targetName)))
	if err != nil {
		return 0, err
	}
	currentTargetPod, err := resourceread.ReadPodV1(rawManifest)
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
