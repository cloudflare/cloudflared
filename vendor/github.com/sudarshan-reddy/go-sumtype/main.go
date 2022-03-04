package main

import (
	"log"
	"os"
	"strings"

	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/packages"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		// TODO: Switch this to use golang.org/x/tools/go/packages.
		log.Fatalf("Usage: go-sumtype <args>\n%s", loader.FromArgsUsage)
	}
	args := os.Args[1:]
	pkgs, err := tycheckAll(args)
	if err != nil {
		log.Fatal(err)
	}
	if errs := run(pkgs); len(errs) > 0 {
		var list []string
		for _, err := range errs {
			list = append(list, err.Error())
		}
		log.Fatal(strings.Join(list, "\n"))
	}
}

func run(pkgs []*packages.Package) []error {
	var errs []error

	decls, err := findSumTypeDecls(pkgs)
	if err != nil {
		return []error{err}
	}

	defs, defErrs := findSumTypeDefs(decls)
	errs = append(errs, defErrs...)
	if len(defs) == 0 {
		return errs
	}

	for _, pkg := range pkgs {
		if pkgErrs := check(pkg, defs); pkgErrs != nil {
			errs = append(errs, pkgErrs...)
		}
	}
	return errs
}

func tycheckAll(args []string) ([]*packages.Package, error) {
	conf := &packages.Config{
		Mode: packages.LoadSyntax,
		// Unfortunately, it appears including the test packages in
		// this lint makes it difficult to do exhaustiveness checking.
		// Namely, it appears that compiling the test version of a
		// package introduces distinct types from the normal version
		// of the package, which will always result in inexhaustive
		// errors whenever a package both defines a sum type and has
		// tests. (Specifically, using `package name`. Using `package
		// name_test` is OK.)
		//
		// It's not clear what the best way to fix this is. :-(
		Tests: false,
	}
	pkgs, err := packages.Load(conf, args...)
	if err != nil {
		return nil, err
	}
	return pkgs, nil
}
