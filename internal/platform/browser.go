package platform

import "fmt"

func OpenBrowser(url string) error {
	if err := openBrowser(url); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
