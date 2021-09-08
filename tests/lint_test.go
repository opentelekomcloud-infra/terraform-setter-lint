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
	"testing"
	"time"

	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint"
	"github.com/stretchr/testify/assert"
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
	var retCode int
	tmpDir, err = os.MkdirTemp("", "tsl-test-*")
	if err != nil {
		log.Fatalf("can't create temp dir for test fixtures: %s", err)
	}
	log.Printf("Temporary test dir created at %s", tmpDir)

	defer func() {
		log.Print("Removing temporary test dir")
		if err := os.RemoveAll(tmpDir); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, err.Error())
		}
		os.Exit(retCode)
	}()

	// copy test fixture files:
	localFixtures := fmt.Sprintf("%s/fixtures", cwd)
	err = copyDir(localFixtures, tmpDir)
	if err != nil {
		log.Fatalf("error copying fixtures to temp dir: %s", err)
	}

	retCode = m.Run()
}

func fixturePath(name string) string {
	return filepath.Join(tmpDir, name)
}

func TestValidatePositive(t *testing.T) {
	assert.NoError(t, lint.Validate(fixturePath("good")))
}

func TestValidateNegative(t *testing.T) {
	assert.Error(t, lint.Validate(fixturePath("loose_assignment")))
}
