package monitor

import (
	"fmt"
	"io/fs"
	"os"
	"testing"
	"time"
)

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
			target := New(nil)
			target.io = scenario.fakeIO
			target.revision = 8
			target.targetName = "kube-apiserver"
			target.staticPodResourcesPath = "/etc/kubernetes/static-pod-resources"
			target.manifestsPath = "/etc/kubernetes/manifests"

			// act
			err := target.createLastKnowGoodRevisionAndDestroy()

			// validate
			if err != nil && len(scenario.expectErr) == 0 {
				t.Fatalf("unexpected error %v", err)
			}
			if err == nil && len(scenario.expectErr) > 0 {
				t.Fatal("expected to get an error")
			}
			if err != nil && err.Error() != scenario.expectErr {
				t.Fatalf("incorrect error: %v, expected: %v", err, scenario.expectErr)
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

	WriteFileFn func(filename string, data []byte, perm fs.FileMode) error
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
	if f.ReadFileFn != nil {
		return f.ReadFileFn(filename)
	}

	return nil, nil
}

func (f *fakeIO) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	if f.WriteFileFn != nil {
		return f.WriteFileFn(filename, data, perm)
	}
	return nil
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
