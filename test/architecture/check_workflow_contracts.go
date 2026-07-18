// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

func main() {
	configuration := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo}
	directories, err := filepath.Glob("domain/*/workflow/*")
	if err != nil {
		fail("Workflow directoryの検索に失敗しました: %v", err)
	}
	patterns := make([]string, 0, len(directories))
	for _, directory := range directories {
		information, statErr := os.Stat(directory)
		if statErr == nil && information.IsDir() {
			patterns = append(patterns, "./"+directory)
		}
	}
	loaded, err := packages.Load(configuration, patterns...)
	if err != nil {
		fail("Workflow packageの読み込みに失敗しました: %v", err)
	}
	if packages.PrintErrors(loaded) != 0 {
		os.Exit(1)
	}
	for _, loadedPackage := range loaded {
		checkPackage(loadedPackage)
	}
}

func checkPackage(loadedPackage *packages.Package) {
	scope := loadedPackage.Types.Scope()
	command := requiredType(scope, loadedPackage.PkgPath, "Command")
	event := requiredType(scope, loadedPackage.PkgPath, "Event")
	workflow := requiredType(scope, loadedPackage.PkgPath, "Workflow")

	method, _, _ := types.LookupFieldOrMethod(types.NewPointer(workflow), true, loadedPackage.Types, "Do")
	if method == nil {
		fail("%s: Workflow.Doがありません", loadedPackage.PkgPath)
	}
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Params().Len() != 2 || signature.Results().Len() != 1 {
		fail("%s: DoはcontextとCommandを受け取りResultを1つ返す必要があります", loadedPackage.PkgPath)
	}
	if !types.Identical(signature.Params().At(1).Type(), command) {
		fail("%s: Doの第2引数はpackage固有のCommandである必要があります", loadedPackage.PkgPath)
	}

	result, ok := signature.Results().At(0).Type().(*types.Named)
	if !ok || result.Obj().Pkg() == nil || !strings.HasSuffix(result.Obj().Pkg().Path(), "/domain/shared/result") || result.Obj().Name() != "Result" {
		fail("%s: Doはdomain/shared/result.Resultを返す必要があります", loadedPackage.PkgPath)
	}
	typeArguments := result.TypeArgs()
	if typeArguments == nil || typeArguments.Len() != 2 || !types.Identical(typeArguments.At(0), event) {
		fail("%s: Resultの成功型はpackage固有のEventである必要があります", loadedPackage.PkgPath)
	}
	failure, ok := typeArguments.At(1).(*types.Named)
	if !ok || failure.Obj().Pkg() == nil || !strings.HasSuffix(failure.Obj().Pkg().Path(), "/domain/shared/workflow") || failure.Obj().Name() != "Failure" {
		fail("%s: Resultの失敗型はdomain/shared/workflow.Failureである必要があります", loadedPackage.PkgPath)
	}
}

func requiredType(scope *types.Scope, packagePath string, name string) *types.Named {
	object := scope.Lookup(name)
	if object == nil {
		fail("%s: %s型がありません", packagePath, name)
	}
	named, ok := object.Type().(*types.Named)
	if !ok {
		fail("%s: %sはnamed typeである必要があります", packagePath, name)
	}
	return named
}

func fail(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
