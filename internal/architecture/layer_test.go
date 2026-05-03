package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/walnuts1018/cluster-api-provider-tart"

func TestInternalLayersDoNotDependOnOuterOrSameLayerPackages(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	var violations []string

	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		currentImportPath := packageImportPath(rel)
		currentLayer, currentLayerPackage := layerPackage(currentImportPath)
		if currentLayer == "" {
			return nil
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			if violation := dependencyViolation(currentLayer, currentLayerPackage, importPath); violation != "" {
				violations = append(violations, rel+": "+violation)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("failed to inspect internal imports: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("layer dependency violations:\n%s", strings.Join(violations, "\n"))
	}
}

func packageImportPath(filePath string) string {
	dir := filepath.Dir(filepath.ToSlash(filePath))
	return modulePath + "/" + dir
}

func layerPackage(importPath string) (string, string) {
	for _, layer := range []string{"domain", "application", "adapter"} {
		prefix := modulePath + "/internal/" + layer + "/"
		if strings.HasPrefix(importPath, prefix) {
			rest := strings.TrimPrefix(importPath, prefix)
			return layer, strings.Split(rest, "/")[0]
		}
	}
	return "", ""
}

func dependencyViolation(currentLayer, currentLayerPackage, importPath string) string {
	if !strings.HasPrefix(importPath, modulePath+"/internal/") {
		if currentLayer == "domain" || currentLayer == "application" {
			if strings.HasPrefix(importPath, "sigs.k8s.io/controller-runtime") {
				return "inner layer imports controller-runtime: " + importPath
			}
		}
		return ""
	}

	importedLayer, importedLayerPackage := layerPackage(importPath)
	switch currentLayer {
	case "domain":
		if importedLayer == "application" || importedLayer == "adapter" {
			return "domain imports outer layer: " + importPath
		}
		if importedLayer == "domain" && importedLayerPackage != currentLayerPackage {
			return "domain package imports sibling domain package: " + importPath
		}
	case "application":
		if importedLayer == "adapter" {
			return "application imports adapter layer: " + importPath
		}
		if importedLayer == "application" && importedLayerPackage != currentLayerPackage {
			return "application package imports sibling application package: " + importPath
		}
	case "adapter":
		if importedLayer == "adapter" && importedLayerPackage != currentLayerPackage {
			return "adapter package imports sibling adapter package: " + importPath
		}
	}
	return ""
}
