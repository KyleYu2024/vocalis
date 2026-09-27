// Package natsort provides human friendly ordering, so that "第2章" sorts
// before "第10章".
package natsort

import "unicode"

// Less reports whether a should be sorted before b.
func Less(a, b string) bool {
	ar, br := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ar) && j < len(br) {
		ca, cb := ar[i], br[j]
		if unicode.IsDigit(ca) && unicode.IsDigit(cb) {
			// Compare the full digit runs numerically, ignoring leading zeros.
			startA, startB := i, j
			for i < len(ar) && unicode.IsDigit(ar[i]) {
				i++
			}
			for j < len(br) && unicode.IsDigit(br[j]) {
				j++
			}
			na := trimZeros(ar[startA:i])
			nb := trimZeros(br[startB:j])
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			for k := range na {
				if na[k] != nb[k] {
					return na[k] < nb[k]
				}
			}
			continue
		}
		la, lb := unicode.ToLower(ca), unicode.ToLower(cb)
		if la != lb {
			return la < lb
		}
		i++
		j++
	}
	return len(ar)-i < len(br)-j
}

func trimZeros(r []rune) []rune {
	k := 0
	for k < len(r)-1 && r[k] == '0' {
		k++
	}
	return r[k:]
}
