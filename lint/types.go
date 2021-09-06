package lint

// Field is a single field description
type Field struct {
	Type     string
	ReadOnly bool
}

// ResourceSchema is a storage for all top-level resource fields
type ResourceSchema struct {
	Fields map[string]Field
}

// ArgumentNames returns writeable resource attribute names
func (r ResourceSchema) ArgumentNames() []string {
	var names []string
	for k, v := range r.Fields {
		if !v.ReadOnly {
			names = append(names, k)
		}
	}
	return names
}

func (r ResourceSchema) AttributeNames() []string {
	var names []string
	for k, _ := range r.Fields {
		names = append(names, k)
	}
	return names
}
