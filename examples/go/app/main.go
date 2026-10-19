// A small licensed application: activation input, status, and a real protected export.
package main

import (
	"bufio"
	"context"
	"fmt"
	licentivo "github.com/spf86/licentivo-sdk"
	"os"
	"strings"
)

// BEGIN LICENTIVO PRODUCT
// Copy Product ID and all key ID/public key pairs from Console > Products > Signing public keys.
const productID = "YOUR_PRODUCT_ID"

var trustedKeys = map[string]string{"YOUR_KEY_ID": "YOUR_PUBLIC_KEY"}

// END LICENTIVO PRODUCT

// A GUI can use the same initialization and business callbacks.
func main() {
	input := bufio.NewReader(os.Stdin)
	fmt.Print("License key (empty to restore this device): ")
	key, _ := input.ReadString('\n')
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := licentivo.OpenProduct(ctx, productID, trustedKeys, key, func(err error) { fmt.Println("Verification:", err) })
	if err != nil {
		fmt.Println(err)
		return
	}
	defer c.Close()
	for {
		fmt.Println("1 Status  2 Export CSV  3 Pro report  4 Refresh license  5 Release device  0 Exit")
		command, _ := input.ReadString('\n')
		switch strings.TrimSpace(command) {
		case "0", "":
			return
		case "1":
			fmt.Printf("%+v\n", c.Status())
		case "2":
			err = c.RunFeature("export", func() error {
				return os.WriteFile("licensed-report.csv", []byte("Item,Quantity,Price\nNotebook,2,12.50\nPen,5,3.00\n"), 0600)
			})
		case "3":
			err = c.RunFeature("pro", func() error { return os.WriteFile("pro-report.txt", []byte("Sales total: 40.00\n"), 0600) })
		case "4":
			_, err = c.RefreshOnline(ctx)
		case "5":
			err = c.Deactivate(ctx)
		}
		if err != nil {
			fmt.Println("Operation blocked or failed:", err)
			err = nil
		}
	}
}
