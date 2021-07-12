package monitor

import (
	"fmt"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"k8s.io/apimachinery/pkg/api/equality"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

var samplePod = `
apiVersion: v1
kind: Pod
metadata:
  name: kube-apiserver
`

func TestFallbackToPreviousRevision(t *testing.T) {
	scenarios := []struct {
		name        string
		fakeIO      *fakeIO
		expectedErr string
	}{
		// scenario 1
		{
			name: "happy path",
			fakeIO: &fakeIO{
				ExpectedStatFnCounter: 1, ExpectedReadFileFnCounter: 1, ExpectedWriteFileFnCounter: 1, ExpectedRemoveFnCounter: 1,
				StatFn: func(path string) (os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return fakeFile("/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good"), nil
				},
				ReadFileFn: func(path string) ([]byte, error) {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []byte(samplePod), nil
				},
				WriteFileFn: func(filename string, data []byte, perm fs.FileMode) error {
					if filename != "/etc/kubernetes/manifests/kube-apiserver-pod.yaml" {
						return fmt.Errorf("unexpected path %s", filename)
					}
					actualPod, err := resourceread.ReadPodV1(data)
					if err != nil {
						return err
					}
					expectedPod, err := resourceread.ReadPodV1([]byte(samplePod))
					if err != nil {
						return err
					}
					expectedPod.UID = actualPod.UID
					expectedPod.Annotations = map[string]string{}
					expectedPod.Annotations["startup-monitor.static-pods.openshift.io/fallback-for-revision"] = "8"
					if !equality.Semantic.DeepEqual(actualPod, expectedPod) {
						return fmt.Errorf("unexpected pod was written")
					}
					return nil
				},
			},
		},

		// scenario 2
		{
			name: "last known doesn't exist",
			fakeIO: &fakeIO{
				ExpectedStatFnCounter: 2, ExpectedReadDirFnCounter: 1, ExpectedWriteFileFnCounter: 1, ExpectedRemoveFnCounter: 1, ExpectedReadFileFnCounter: 1, ExpectedSymlinkFnCounter: 1,
				StatFn: func(path string) (os.FileInfo, error) {
					switch path {
					// first call
					case "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good":
						return nil, os.ErrNotExist
					// second call
					case "/etc/kubernetes/static-pod-resources/kube-apiserver-pod-9/kube-apiserver-pod.yaml":
						return fakeFile("/etc/kubernetes/static-pod-resources/kube-apiserver-pod-9/kube-apiserver-pod.yaml"), nil
					default:
						return nil, fmt.Errorf("unexpected path %s", path)
					}
				},
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []os.FileInfo{fakeDir("kube-apiserver-pod-7"), fakeDir("kube-apiserver-pod-12"), fakeDir("kube-apiserver-pod-9")}, nil
				},
				SymlinkFn: func(oldname, newname string) error {
					if oldname != "/etc/kubernetes/static-pod-resources/kube-apiserver-pod-9/kube-apiserver-pod.yaml" {
						return fmt.Errorf("unexpected oldname %s", oldname)
					}
					if newname != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return fmt.Errorf("unexpected newname %s", newname)
					}
					return nil
				},
				ReadFileFn: func(path string) ([]byte, error) {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []byte(samplePod), nil
				},
				WriteFileFn: func(filename string, data []byte, perm fs.FileMode) error {
					if filename != "/etc/kubernetes/manifests/kube-apiserver-pod.yaml" {
						return fmt.Errorf("unexpected path %s", filename)
					}
					actualPod, err := resourceread.ReadPodV1(data)
					if err != nil {
						return err
					}
					expectedPod, err := resourceread.ReadPodV1([]byte(samplePod))
					if err != nil {
						return err
					}
					expectedPod.UID = actualPod.UID
					expectedPod.Annotations = map[string]string{}
					expectedPod.Annotations["startup-monitor.static-pods.openshift.io/fallback-for-revision"] = "8"
					if !equality.Semantic.DeepEqual(actualPod, expectedPod) {
						return fmt.Errorf("unexpected pod was written")
					}
					return nil
				},
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// test data
			target := createTestTarget(scenario.fakeIO)

			// act
			err := target.fallbackToPreviousRevision()
			validateError(t, err, scenario.expectedErr)
			if err := scenario.fakeIO.Validate(); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestFindPreviousRevision(t *testing.T) {
	scenarios := []struct {
		name   string
		fakeIO *fakeIO

		expectedPrevRev int
		expectedErr     string
		expectedFound   bool
	}{
		// scenario 1
		{
			name: "ReadDir error",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return nil, fmt.Errorf("fake error")
				},
			},
			expectedErr: "fake error",
		},

		// scenario 2
		{
			name: "ReadDir returns empty result",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return nil, nil
				},
			},
		},

		// scenario 3
		{
			name: "ReadDir returns files only",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []os.FileInfo{fakeFile("kube-apiserver-pod-11"), fakeFile("kube-apiserver-pod-12")}, nil
				},
			},
		},

		// scenario 4
		{
			name: "ReadDir returns a directory that doesn't match prefix",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []os.FileInfo{fakeDir("kube-abc-apiserver-pod-11")}, nil
				},
			},
		},

		// scenario 5
		{
			name: "ReadDir returns a directory that has incorrect revision",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []os.FileInfo{fakeDir("kube-apiserver-pod-FF")}, nil
				},
			},
			expectedErr: `strconv.Atoi: parsing "FF": invalid syntax`,
		},

		// scenario 6
		{
			name: "ReadDir returns a single directory",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []os.FileInfo{fakeDir("kube-apiserver-pod-11")}, nil
				},
			},
		},

		// scenario 7
		{
			name: "prev rev found",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []os.FileInfo{fakeDir("kube-apiserver-pod-11"), fakeDir("kube-apiserver-pod-12")}, nil
				},
			},
			expectedPrevRev: 11,
			expectedFound:   true,
		},

		// scenario 8
		{
			name: "prev rev found with sort",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []os.FileInfo{fakeDir("kube-apiserver-pod-12"), fakeDir("kube-apiserver-pod-9")}, nil
				},
			},
			expectedPrevRev: 9,
			expectedFound:   true,
		},

		// scenario 9
		{
			name: "prev rev found with files that match the prefix",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []os.FileInfo{fakeDir("kube-apiserver-pod-12"), fakeDir("kube-apiserver-pod-11"), fakeFile("kube-apiserver-pod-13"), fakeFile("kube-apiserver-pod-14")}, nil
				},
			},
			expectedPrevRev: 11,
			expectedFound:   true,
		},

		// scenario 10
		{
			name: "ReadDir returns an incorrect directory",
			fakeIO: &fakeIO{
				ExpectedReadDirFnCounter: 1,
				ReadDirFn: func(path string) ([]os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return []os.FileInfo{fakeDir("kube-apiserver-abc-11")}, nil
				},
			},
			expectedErr: "unable to extract revision from kube-apiserver-abc-11 due to incorrect format",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// test data
			target := createTestTarget(scenario.fakeIO)

			// act
			prevRev, found, err := target.findPreviousRevision()

			// validate
			if err := scenario.fakeIO.Validate(); err != nil {
				t.Error(err)
			}
			if prevRev != scenario.expectedPrevRev {
				t.Errorf("unexpected prevRev %d, expected %d", prevRev, scenario.expectedPrevRev)
			}
			if found != scenario.expectedFound {
				t.Errorf("unexpected found %v, expected %v", found, scenario.expectedFound)
			}
			validateError(t, err, scenario.expectedErr)
		})
	}
}

func TestCreateLastKnowGoodRevisionAndExit(t *testing.T) {
	scenarios := []struct {
		name      string
		fakeIO    *fakeIO
		expectErr string
	}{
		// scenario 1
		{
			name: "step 0: is a dir",
			fakeIO: &fakeIO{
				ExpectedStatFnCounter: 1,
				StatFn: func(path string) (os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return fakeDir("fake-directory"), nil
				},
			},
			expectErr: "the provided path /etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good is incorrect and points to a directory",
		},

		// scenario 2
		{
			name: "step 0: rm fails",
			fakeIO: &fakeIO{
				ExpectedStatFnCounter:   1,
				ExpectedRemoveFnCounter: 1,

				StatFn: func(path string) (os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return fakeFile("fake-file"), nil
				},
				RemoveFn: func(path string) error {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return fmt.Errorf("unexpected path %s", path)
					}
					return fmt.Errorf("fake error")
				},
			},
			expectErr: "fake error",
		},

		// scenario 3
		{
			name: "step 0: !IsNotExists",
			fakeIO: &fakeIO{
				ExpectedStatFnCounter: 1,
				StatFn: func(path string) (os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return fakeFile("fake-file"), fmt.Errorf("fake error")
				},
			},
			expectErr: "fake error",
		},

		// scenario 4
		{
			name: "step 1: SymLink err",
			fakeIO: &fakeIO{
				ExpectedStatFnCounter:    1,
				ExpectedSymlinkFnCounter: 1,
				StatFn: func(path string) (os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return fakeFile("fake-file"), os.ErrNotExist
				},
				SymlinkFn: func(oldname, newname string) error {
					if oldname != "/etc/kubernetes/static-pod-resources/kube-apiserver-pod-8/kube-apiserver-pod.yaml" {
						return fmt.Errorf("unexpected oldname %s", oldname)
					}
					if newname != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return fmt.Errorf("unexpected newname %s", newname)
					}
					return fmt.Errorf("fake err")
				},
			},
			expectErr: `failed to create a symbolic link "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" for "/etc/kubernetes/static-pod-resources/kube-apiserver-pod-8/kube-apiserver-pod.yaml" due to fake err`,
		},

		// scenario 5
		{
			name: "step 2: suicide err",
			fakeIO: &fakeIO{
				ExpectedStatFnCounter:    1,
				ExpectedSymlinkFnCounter: 1,
				ExpectedRemoveFnCounter:  1,
				StatFn: func(path string) (os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return fakeFile("fake-file"), os.ErrNotExist
				},
				SymlinkFn: func(oldname, newname string) error {
					if oldname != "/etc/kubernetes/static-pod-resources/kube-apiserver-pod-8/kube-apiserver-pod.yaml" {
						return fmt.Errorf("unexpected oldname %s", oldname)
					}
					if newname != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return fmt.Errorf("unexpected newname %s", newname)
					}
					return nil
				},
				RemoveFn: func(path string) error {
					if path != "/etc/kubernetes/manifests/kube-apiserver-startup-monitor.yaml" {
						return fmt.Errorf("unexpected path %s", path)
					}
					return fmt.Errorf("fake error")
				},
			},
			expectErr: "fake error",
		},

		// scenario 6
		{
			name: "happy path",
			fakeIO: &fakeIO{
				ExpectedStatFnCounter:    1,
				ExpectedSymlinkFnCounter: 1,
				ExpectedRemoveFnCounter:  1,
				StatFn: func(path string) (os.FileInfo, error) {
					if path != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return nil, fmt.Errorf("unexpected path %s", path)
					}
					return fakeFile("fake-file"), os.ErrNotExist
				},
				SymlinkFn: func(oldname, newname string) error {
					if oldname != "/etc/kubernetes/static-pod-resources/kube-apiserver-pod-8/kube-apiserver-pod.yaml" {
						return fmt.Errorf("unexpected oldname %s", oldname)
					}
					if newname != "/etc/kubernetes/static-pod-resources/kube-apiserver-last-known-good" {
						return fmt.Errorf("unexpected newname %s", newname)
					}
					return nil
				},
				RemoveFn: func(path string) error {
					if path != "/etc/kubernetes/manifests/kube-apiserver-startup-monitor.yaml" {
						return fmt.Errorf("unexpected path %s", path)
					}
					return nil
				},
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// test data
			target := createTestTarget(scenario.fakeIO)

			// act
			err := target.createLastKnowGoodRevisionAndDestroy()

			// validate
			validateError(t, err, scenario.expectErr)
			if err := scenario.fakeIO.Validate(); err != nil {
				t.Error(err)
			}

			if scenario.fakeIO.ExpectedStatFnCounter != scenario.fakeIO.StatFnCounter {
				t.Errorf("unexpected StatFn inovations %d, expeccted %d", scenario.fakeIO.StatFnCounter, scenario.fakeIO.ExpectedStatFnCounter)
			}
			if scenario.fakeIO.ExpectedSymlinkFnCounter != scenario.fakeIO.SymlinkFnCounter {
				t.Errorf("unexpected SymlinkFn inovations %d, expeccted %d", scenario.fakeIO.SymlinkFnCounter, scenario.fakeIO.ExpectedSymlinkFnCounter)
			}
			if scenario.fakeIO.ExpectedRemoveFnCounter != scenario.fakeIO.RemoveFnCounter {
				t.Errorf("unexpected RemoveFn inovations %d, expeccted %d", scenario.fakeIO.RemoveFnCounter, scenario.fakeIO.ExpectedRemoveFnCounter)
			}

		})
	}
}

func TestLoadTargetManifestAndExtractRevision(t *testing.T) {
	scenarios := []struct {
		name             string
		goldenFilePrefix string
		expectedRev      int
		expectError      bool
	}{

		// scenario 1
		{
			name:             "happy path: a revision is extracted",
			goldenFilePrefix: "scenario-1",
			expectedRev:      8,
		},

		// scenario 2
		{
			name:             "the target pod doesn't have a revision label",
			goldenFilePrefix: "scenario-2",
			expectError:      true,
		},

		// scenario 3
		{
			name:             "the target pod has an incorrect label",
			goldenFilePrefix: "scenario-3",
			expectError:      true,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// test data
			target := New(nil)
			target.manifestsPath = "./testdata"
			target.targetName = scenario.goldenFilePrefix

			// act
			rev, err := target.loadRootTargetPodAndExtractRevision()

			// validate
			if err != nil && !scenario.expectError {
				t.Fatal(err)
			}
			if err == nil && scenario.expectError {
				t.Fatal("expected to get an error")
			}
			if rev != scenario.expectedRev {
				t.Errorf("unexpected rev %d, expected %d", rev, scenario.expectedRev)
			}
		})
	}
}

func createTestTarget(fakeIO *fakeIO) *StartupMonitor {
	target := New(nil)
	target.io = fakeIO
	target.revision = 8
	target.targetName = "kube-apiserver"
	target.staticPodResourcesPath = "/etc/kubernetes/static-pod-resources"
	target.manifestsPath = "/etc/kubernetes/manifests"
	return target
}

type fakeIO struct {
	StatFn                func(string) (os.FileInfo, error)
	StatFnCounter         int
	ExpectedStatFnCounter int

	SymlinkFn                func(string, string) error
	SymlinkFnCounter         int
	ExpectedSymlinkFnCounter int

	RemoveFn                func(string) error
	RemoveFnCounter         int
	ExpectedRemoveFnCounter int

	ReadFileFn func(string) ([]byte, error)
	ReadFileFnCounter int
	ExpectedReadFileFnCounter int

	ReadDirFn  func(string) ([]fs.FileInfo, error)
	ReadDirFnCounter int
	ExpectedReadDirFnCounter int

	WriteFileFn func(filename string, data []byte, perm fs.FileMode) error
	WriteFileFnCounter int
	ExpectedWriteFileFnCounter int
}

func (f *fakeIO) Symlink(oldname string, newname string) error {
	f.SymlinkFnCounter++
	if f.SymlinkFn != nil {
		return f.SymlinkFn(oldname, newname)
	}
	return nil
}

func (f *fakeIO) Stat(path string) (os.FileInfo, error) {
	f.StatFnCounter++
	if f.StatFn != nil {
		return f.StatFn(path)
	}
	return nil, nil
}

func (f *fakeIO) Remove(path string) error {
	f.RemoveFnCounter++
	if f.RemoveFn != nil {
		return f.RemoveFn(path)
	}
	return nil
}

func (f *fakeIO) ReadFile(filename string) ([]byte, error) {
	f.ReadFileFnCounter++
	if f.ReadFileFn != nil {
		return f.ReadFileFn(filename)
	}

	return nil, nil
}

func (f *fakeIO) ReadDir(dirname string) ([]fs.FileInfo, error) {
	f.ReadDirFnCounter++
	if f.ReadDirFn != nil {
		return f.ReadDirFn(dirname)
	}
	return nil, nil
}

func (f *fakeIO) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	f.WriteFileFnCounter++
	if f.WriteFileFn != nil {
		return f.WriteFileFn(filename, data, perm)
	}
	return nil
}

func (f *fakeIO) Validate() error {
	var errs []error
	if f.SymlinkFnCounter != f.ExpectedSymlinkFnCounter {
		errs = append(errs, fmt.Errorf("unexpected SymlinkFnCounter %d, expected %d", f.SymlinkFnCounter, f.ExpectedSymlinkFnCounter))
	}

	if f.StatFnCounter != f.ExpectedStatFnCounter {
		errs = append(errs, fmt.Errorf("unexpected StatFnCounter %d, expected %d", f.StatFnCounter, f.ExpectedStatFnCounter))
	}

	if f.RemoveFnCounter != f.ExpectedRemoveFnCounter {
		errs = append(errs, fmt.Errorf("unexpected RemoveFnCounter %d, expected %d", f.RemoveFnCounter, f.ExpectedRemoveFnCounter))
	}

	if f.ReadFileFnCounter != f.ExpectedReadFileFnCounter {
		errs = append(errs, fmt.Errorf("unexpected ReadFileFnCounter %d, expected %d", f.ReadFileFnCounter, f.ExpectedReadFileFnCounter))
	}

	if f.ReadDirFnCounter != f.ExpectedReadDirFnCounter {
		errs = append(errs, fmt.Errorf("unexpected ReadDirFnCounter %d, expected %d", f.ReadDirFnCounter, f.ExpectedReadDirFnCounter))
	}

	if f.WriteFileFnCounter != f.ExpectedWriteFileFnCounter {
		errs = append(errs, fmt.Errorf("unexpected WriteFileFnCounter %d, expected %d", f.WriteFileFnCounter, f.ExpectedWriteFileFnCounter))
	}

	return kerrors.NewAggregate(errs)
}

type fakeFile string

func (f fakeFile) Name() string       { return string(f) }
func (f fakeFile) Size() int64        { return 0 }
func (f fakeFile) Mode() fs.FileMode  { return fs.ModeAppend }
func (f fakeFile) ModTime() time.Time { return time.Unix(0, 0) }
func (f fakeFile) IsDir() bool        { return false }
func (f fakeFile) Sys() interface{}   { return nil }

type fakeDir string

func (f fakeDir) Name() string       { return string(f) }
func (f fakeDir) Size() int64        { return 0 }
func (f fakeDir) Mode() fs.FileMode  { return fs.ModeDir | 0500 }
func (f fakeDir) ModTime() time.Time { return time.Unix(0, 0) }
func (f fakeDir) IsDir() bool        { return true }
func (f fakeDir) Sys() interface{}   { return nil }

func validateError(t *testing.T, actualErr error, expectedErr string) {
	if actualErr != nil && len(expectedErr) == 0 {
		t.Fatalf("unexpected error %v", actualErr)
	}
	if actualErr == nil && len(expectedErr) > 0 {
		t.Fatal("expected to get an error")
	}
	if actualErr != nil && actualErr.Error() != expectedErr {
		t.Fatalf("incorrect error: %v, expected: %v", actualErr, expectedErr)
	}
}
