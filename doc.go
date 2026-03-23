/*
package weixin provides a small Go client for the ilink Weixin QR-login flow
used by the OpenClaw Weixin plugin.

Minimal usage:

	client := weixin.NewClient(weixin.Options{})
	account, err := client.LoginInteractive(ctx, weixin.InteractiveLoginOptions{
	    Output:  os.Stdout,
	    SaveDir: ".weixin-accounts",
	})
	if err != nil {
	    log.Fatal(err)
	}

The returned account contains the ilink bot token and account identifiers that
can be reused by your own application.
*/
package weixin
