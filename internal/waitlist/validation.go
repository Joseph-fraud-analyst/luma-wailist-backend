package waitlist

// validUUID performs a basic structural validation of a UUID.
// It accepts standard UUID strings in the format:
//
// xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
//
// This helper validates the structure only. It does not restrict
// the UUID version or variant.
func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, character := range value {
		// Standard UUID hyphen positions.
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if character != '-' {
				return false
			}

			continue
		}

		isDigit := character >= '0' && character <= '9'
		isLowerHex := character >= 'a' && character <= 'f'
		isUpperHex := character >= 'A' && character <= 'F'

		if !isDigit && !isLowerHex && !isUpperHex {
			return false
		}
	}

	return true
}
