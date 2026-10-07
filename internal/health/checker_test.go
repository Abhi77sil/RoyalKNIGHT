package health

import (
	"testing"
)

func TestValidateDomain(t *testing.T) {
	validDomains := []string{
		"example.com",
		"sub.example.com",
		"my-site.co.uk",
		"localhost",
		"test1234.org",
	}

	for _, d := range validDomains {
		if valid, err := ValidateDomain(d); !valid {
			t.Errorf("expected %s to be valid, got error: %s", d, err)
		}
	}

	invalidDomains := []string{
		"",
		"-example.com",
		"example-.com",
		"example..com",
		"http://example.com",
		"example.com/test",
		"invalid_domain.com",
	}

	for _, d := range invalidDomains {
		if valid, _ := ValidateDomain(d); valid {
			t.Errorf("expected %s to be invalid, but was marked valid", d)
		}
	}
}
