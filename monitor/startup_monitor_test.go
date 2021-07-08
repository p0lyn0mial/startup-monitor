package monitor

import (
	"os"
	"testing"
)

func TestCreateLastKnowGoodRevisionAndExit(t *testing.T) {
	scenarios := []struct {
		name string
		fakeIO IOInterface
	}{
		// scenario 1
		{
			name: "",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// test data
			target := New(nil)
			target.io = scenario.fakeIO

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
			rev, err := target.loadTargetManifestAndExtractRevision()

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

type fakeOS struct {
	StatFn     func(string) (os.FileInfo, error)
	SymlinkFn  func(string, string) error
	RemoveFn   func(string) error
}

func (f *fakeOS) Symlink(oldname string, newname string) error {
	if f.SymlinkFn != nil {
		return f.SymlinkFn(oldname, newname)
	}
	return nil
}

func (f fakeOS) Stat(path string) (os.FileInfo, error) {
	if f.StatFn != nil {
		return f.StatFn(path)
	}
	return nil, nil
}

func (f *fakeOS) Remove(path string) error {
	if f.RemoveFn != nil {
		return f.RemoveFn(path)
	}
	return nil
}
