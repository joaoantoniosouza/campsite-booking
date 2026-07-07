package archtest

import (
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const modulesPrefix = "github.com/campsite-booking/campsite-booking/internal/modules/"

var moduleLayerRe = regexp.MustCompile(`^` + regexp.QuoteMeta(modulesPrefix) + `([^/]+)/([^/]+)`)

var forbiddenDomainImports = []string{
	"github.com/go-chi/chi",
	"github.com/jackc/pgx",
	"github.com/gorilla/sessions",
	"net/http",
}

func loadModulePackages(t *testing.T) []*packages.Package {
	t.Helper()
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps}
	pkgs, err := packages.Load(cfg, modulesPrefix+"...")
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("expected to load at least one package under internal/modules")
	}
	return pkgs
}

func moduleAndLayer(pkgPath string) (module, layer string, ok bool) {
	m := moduleLayerRe.FindStringSubmatch(pkgPath)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func TestBoundary_NoCrossModuleImports(t *testing.T) {
	pkgs := loadModulePackages(t)

	for _, pkg := range pkgs {
		module, _, ok := moduleAndLayer(pkg.PkgPath)
		if !ok {
			continue
		}
		for importPath := range pkg.Imports {
			otherModule, otherLayer, ok := moduleAndLayer(importPath)
			if !ok || otherModule == module {
				continue
			}
			if otherLayer == "domain" || otherLayer == "app" {
				t.Errorf("%s imports %s: module %q may not import another module's %s layer",
					pkg.PkgPath, importPath, module, otherLayer)
			}
		}
	}
}

func TestBoundary_DomainLayerPurity(t *testing.T) {
	pkgs := loadModulePackages(t)

	for _, pkg := range pkgs {
		module, layer, ok := moduleAndLayer(pkg.PkgPath)
		if !ok || layer != "domain" {
			continue
		}
		for importPath := range pkg.Imports {
			for _, forbidden := range forbiddenDomainImports {
				if strings.HasPrefix(importPath, forbidden) {
					t.Errorf("%s (domain layer of %q) imports forbidden package %s", pkg.PkgPath, module, importPath)
				}
			}
			if otherModule, _, ok := moduleAndLayer(importPath); ok && otherModule != module {
				t.Errorf("%s (domain layer of %q) imports another module's package %s", pkg.PkgPath, module, importPath)
			}
		}
	}
}
