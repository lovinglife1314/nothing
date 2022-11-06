package service

type Roles []string

func (rs Roles) Support(ors Roles) bool {
	if len(rs) == 0 || len(ors) == 0 {
		return true
	}

	for _, or := range ors {
		for _, r := range rs {
			if or == r {
				return true
			}
		}
	}
	return false
}

