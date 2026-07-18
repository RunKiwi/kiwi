package main

import "flag"

// parseFlagsAnywhere parses args allowing flags to appear before, after, or
// interleaved with positional arguments. The standard flag package stops at the
// first positional token, so `creds set git <token> -server X` would silently
// drop -server; this walks the args, consuming one positional at a time and
// re-parsing the remainder so flags in any position are honored. It returns the
// positional arguments in order.
func parseFlagsAnywhere(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
}
