// Code generated by "stringer -type Migration -linecomment"; DO NOT EDIT.

package cmd

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[MAuto-0]
	_ = x[MUser1-1]
	_ = x[MCount-2]
}

const _Migration_name = "MAutoMUser1MCount"

var _Migration_index = [...]uint8{0, 5, 11, 17}

func (i Migration) String() string {
	if i < 0 || i >= Migration(len(_Migration_index)-1) {
		return "Migration(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _Migration_name[_Migration_index[i]:_Migration_index[i+1]]
}
