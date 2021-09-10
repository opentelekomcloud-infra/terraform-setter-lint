package utils

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

func UnwrapString(lit *ast.BasicLit) (string, error) {
	if lit.Kind != token.STRING {
		return "", fmt.Errorf("value is not a string")
	}
	return strings.Trim(lit.Value, `"'`), nil
}
