package blacklist

import (
	"log"
	"regexp"
	"strings"

	"github.com/mattn/go-shellwords"
)

// Blacklist represents a list of regex meant filter out logs.
type Blacklist struct {
	regexp []*regexp.Regexp
	//multi map string -> regexp
	regexpMap map[string][]*regexp.Regexp
}

//IsBlacklisted Return true if any regex in the regex array match this line
func (b *Blacklist) IsBlacklisted(line []byte) bool {
	for _, r := range b.regexp {
		if r.Match(line) {
			return true
		}
	}
	return false
}

//PrepareBlacklist Parse the string using ; separator and return the Blacklist struct
func PrepareBlacklist(blacklist *string) Blacklist {

	b := Blacklist{}
	b.regexpMap = make(map[string][]*regexp.Regexp)
	args, _ := shellwords.Parse(*blacklist)
	for _, r := range args {
		if len(r) > 0 {
			r = strings.Replace(r, "==", "  ", -1)
			splitted := strings.Split(r, "=")
			if len(splitted) == 1 {
				pattern := strings.Replace(r, "  ", "=", -1)
				rexp := regexp.MustCompile(pattern)
				b.regexp = append(b.regexp, rexp)
			} else if len(splitted) == 2 {
				field := splitted[0]
				pattern := splitted[1]
				rexp := regexp.MustCompile(pattern)
				b.regexpMap[field] = append(b.regexpMap[field], rexp)
			} else {
				log.Fatal("Illegal input syntax for blacklist")
			}
		}
	}
	return b
}
