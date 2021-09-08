package set

type StringSet struct {
	hash map[string]struct{}
}

func (s *StringSet) Contains(el string) bool {
	_, ok := s.hash[el]
	return ok
}

func SetFromSlice(slc []string) StringSet {
	set := StringSet{hash: map[string]struct{}{}}
	for _, v := range slc {
		set.hash[v] = struct{}{}
	}
	return set
}
