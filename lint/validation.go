package lint

import (
	"fmt"
	"go/token"
	"log"

	"github.com/hashicorp/go-multierror"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/core"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/parser"
	"golang.org/x/tools/go/packages"
)

// Validate searches for all resource and validate their setters
func Validate(path string) error {
	fSet := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedDeps |
			packages.NeedImports |
			packages.NeedSyntax,
		Fset: fSet,
		Dir:  path,
	}
	log.Println("Start validating packages at", path)
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return fmt.Errorf("error loading packages: %w", err)
	}

	var mErr *multierror.Error
	pkgCache := map[string]*core.Scope{}
	for _, pkg := range pkgs {
		p := parser.NewParser(pkg, fSet, pkgCache) // we need this state to use types and imports later
		mErr = multierror.Append(mErr, p.Validate())
	}
	return mErr.ErrorOrNil()
}
