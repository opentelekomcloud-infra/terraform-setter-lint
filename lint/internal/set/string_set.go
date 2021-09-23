package set

type StringSet struct {
	hash map[string]struct{}
}

func (s *StringSet) Push(v ...string) {
	if s.hash == nil {
		s.hash = map[string]struct{}{}
	}

	for _, stV := range v {
		s.hash[stV] = struct{}{}
	}
}

func (s *StringSet) Contains(el string) bool {
	_, ok := s.hash[el]
	return ok
}

func StringSetFromSlice(slc []string) *StringSet {
	set := &StringSet{}
	set.Push(slc...)
	return set
}
