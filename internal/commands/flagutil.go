package commands

// hoistFlags reorders args so option flags (and the values of flags that take
// one) precede positional arguments. Go's stdlib flag package stops parsing at
// the first positional argument, silently swallowing flags typed after one —
// `moxie scan <dir> --force` used to treat --force as a scan directory, and
// `moxie sync <id> --force` simply ignored it. needsValue lists the flags that
// consume the next argument as their value.
//
// A leading "--" terminates flag processing: everything after it is treated as
// positional, matching the flag package's own contract.
func hoistFlags(args []string, needsValue map[string]bool) []string {
	var flags, positionals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			if needsValue[a] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positionals = append(positionals, a)
	}
	return append(flags, positionals...)
}

// scanValueFlags are the scan flags that take a following argument as their
// value; hoistFlags must move that value along with the flag.
var scanValueFlags = map[string]bool{
	"--engine":      true,
	"--cookie":      true,
	"--cookie-file": true,
}

// syncValueFlags cover sync and check-updates.
var syncValueFlags = map[string]bool{
	"--cookie":      true,
	"--cookie-file": true,
	"--parallel":    true,
}