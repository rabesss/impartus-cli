package library

const privateWindowsDACLProtected uint16 = 0x1000

func privateWindowsDACLIsProtected(control uint16) bool {
	return control&privateWindowsDACLProtected != 0
}
