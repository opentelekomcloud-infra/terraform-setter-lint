package lint

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
)

type ErrValidation struct {
	file   string
	fields []string
}

func (e ErrValidation) Error() string {
	return fmt.Sprintf("File %s contains following setters for non-existent fields: %v", e.file, e.fields)
}

// Validate searches for all resource and validate their setters
func Validate(path string) error {
	generators, err := ResourceGenerators(path)
	if err != nil {
		return err
	}
	mErr := &multierror.Error{}
	for file, generator := range generators {
		schema, err := GetGeneratorSchema(generator)
		if err != nil {
			return err
		}
		setters, err := FindFieldSetters(file)
		if err != nil {
			return err
		}
		var fields []string
		for _, field := range setters {
			if _, exists := schema.Fields[field]; !exists {
				fields = append(fields, field)
			}
		}
		if fields != nil {
			mErr = multierror.Append(mErr, ErrValidation{file, fields})
		}
	}
	return mErr.ErrorOrNil()
}
