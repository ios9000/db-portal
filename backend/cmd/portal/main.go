// Command portal is the DB Portal server binary (real server arrives with WU-003).
package main

import (
	"fmt"

	"github.com/ios9000/db-portal/backend/internal/version"
)

func main() {
	fmt.Println("db-portal", version.Version)
}
