package tests

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var cwd string
var tmpDir string

func init() {
	rand.Seed(time.Now().UnixNano())

	wd, err := os.Getwd()
	if err != nil {
		panic("can't get CWD")
	}
	cwd = wd
}

func copyFile(src, dest string) error {
	b, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(dest, b, 0600)
}

func copyDir(src, dest string) error {
	return filepath.Walk(src, func(p string, i fs.FileInfo, err error) error {
		if i.IsDir() {
			return nil
		}
		rDest, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(rDest, ".tmpl") {
			return nil
		}
		rDest = strings.Replace(rDest, ".tmpl", "", 1)
		destFile := filepath.Join(dest, rDest)
		if err := os.MkdirAll(filepath.Dir(destFile), 0700); err != nil {
			return err
		}
		return copyFile(p, destFile)
	})
}

func TestMain(m *testing.M) {
	var err error
	tmpDir, err = os.MkdirTemp("", "tsl-test-*")
	if err != nil {
		log.Fatalf("can't create temp dir for test fixtures: %s", err)
	}
	log.Printf("temporary test dir created at %s", tmpDir)

	// copy test fixture files:
	localFixtures := fmt.Sprintf("%s/fixtures", cwd)
	err = copyDir(localFixtures, tmpDir)
	if err != nil {
		log.Fatalf("error copying fixtures to temp dir: %s", err)
	}

	r := m.Run()
	if err := os.RemoveAll(tmpDir); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, err.Error())
	}
	os.Exit(r)
}

func fixturePath(name string) string {
	return filepath.Join(tmpDir, name)
}

func TestListingResourceGenerators(t *testing.T) {
	gens, err := lint.ResourceGenerators(fixturePath("good"))
	require.NoError(t, err)
	assert.Len(t, gens, 1)
}

func TestListingNoResourceGenerators(t *testing.T) {
	gens, err := lint.ResourceGenerators(fixturePath("no_resources"))
	require.NoError(t, err)
	assert.Len(t, gens, 0)
}

func prepareComplex(t *testing.T, count int) {
	baseFile := filepath.Join(fixturePath("good"), "example.go")
	bytesBase, err := ioutil.ReadFile(baseFile)
	require.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(count)
	for i := 0; i < count; i++ {
		firstPath := filepath.Join(fixturePath("complex"), fmt.Sprintf("pack%d", i))
		require.NoError(t, os.MkdirAll(firstPath, 0700))
		require.NoError(t, ioutil.WriteFile(filepath.Join(firstPath, "example.go"), bytesBase, 0700))
		wg.Done()
	}
	wg.Wait()
}

func cleanupComplex(t *testing.T) {
	require.NoError(t, os.RemoveAll(fixturePath("complex")))
}

func TestListingDeepPackages(t *testing.T) {
	count := rand.Intn(10)
	t.Logf("Will generate %d files", count)

	prepareComplex(t, count)
	defer cleanupComplex(t)

	gens, err := lint.ResourceGenerators(fixturePath("complex"))
	require.NoError(t, err)
	assert.Len(t, gens, count)
}

// TestListingSchemaFields checks that schema fields are listed correctly
func TestListingSchemaFields(t *testing.T) {
	gens, err := lint.ResourceGenerators(fixturePath("good"))
	assert.Len(t, gens, 1)

	filePath := filepath.Join(fixturePath("good"), "example.go")
	sch, err := lint.GetGeneratorSchema(gens[filePath])
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"domain_id", "group_id", "project_id", "role_id", "user_id"},
		sch.ArgumentNames(),
	)
	assert.ElementsMatch(t,
		[]string{"user_id", "domain_id", "group_id", "project_id", "role_id", "read_only_field"},
		sch.AttributeNames(),
	)
}

func TestFieldsFromCall(t *testing.T) {
	gens, err := lint.ResourceGenerators(fixturePath("call"))
	assert.Len(t, gens, 1)

	filePath := filepath.Join(fixturePath("call"), "example.go")
	sch, err := lint.GetGeneratorSchema(gens[filePath])
	require.NoError(t, err)

	assert.Equal(t, "TypeMap", sch.Fields["tags"].Type)
	assert.Equal(t, false, sch.Fields["tags"].ReadOnly)
}

func TestFindFieldSetters(t *testing.T) {
	filePath := filepath.Join(fixturePath("good"), "example.go")
	setters, err := lint.FindFieldSetters(filePath)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"domain_id", "project_id", "group_id", "user_id", "role_id"}, setters)
}

func TestValidatePositive(t *testing.T) {
	assert.NoError(t, lint.Validate(fixturePath("good")))
}

func TestValidateNegative(t *testing.T) {
	assert.Error(t, lint.Validate(fixturePath("loose_assignment")))
}
