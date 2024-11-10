package commons

import (
	"fmt"
	irodsclient_fs "github.com/cyverse/go-irodsclient/fs"
	"github.com/dlclark/regexp2"
	"regexp"
)

func ExpandWildcards(fs *irodsclient_fs.FileSystem, input []string, expand_collections bool, expand_dataobjects bool) {
	fmt.Println("Expand wildcards called")
	return
}

func hasWildCards(input string) bool {
	return (regexp.MustCompile(`(?:!\\)(?:\\\\)*[?*]`).MatchString(input) ||
		regexp.MustCompile(`^(?:\\\\)*[?*]`).MatchString(input) ||
		regexp.MustCompile(`(?:!\\)(?:\\\\)*\[.*?(?:!\\)(?:\\\\)*\]`).MatchString(input) ||
		regexp.MustCompile(`^(?:\\\\)*\[.*?(?:!\\)(?:\\\\)*\]`).MatchString(input))
}

func unixWildcardsToSQLWildcards(input string) string {
	output := input
	length := len(input)
	// Use regexp2 rather than regexp here in order to be able to use lookbehind assertions
	//
	// Escape SQL wildcard characters
	output = strings.ReplaceAll(output, "%", `\%`)
	output = strings.ReplaceAll(output, "_", `\_`)
	// Replace ranges with a wildcard
	output, _ = regexp2.MustCompile(`(?<!\\)(?:\\\\)*\[.*?(?<!\\)(?:\\\\)*\]`, regexp2.RE2).Replace(output, `_`, 0, length)
	// Replace non-escaped regular wildcard characters with SQL equivalents
	output, _ = regexp2.MustCompile(`(?<!\\)(?:\\\\)*([*?])`, regexp2.RE2).Replace(output, `\$1`, 0, length)
	return output
}
