package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/Immortal-Protocols/Chariot-CLI/internal/demo"
	"github.com/spf13/cobra"
)

var demoServePort int

var demoServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run a local webhook receiver that prints agent replies (demo only)",
	Long: `Run a local webhook receiver that prints agent replies.

Accepts the POSTs Chariot makes to a deploy's --endpoint and prints each one.
The hosted backend can only reach a public URL — for a local demo, expose this
port with a tunnel (e.g. ngrok, cloudflared) and deploy with the tunnel URL as
--endpoint.

Demo only — in production your own service is the webhook receiver. Run
` + "`chariot api`" + ` for the delivery payload it must accept.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		addr := fmt.Sprintf(":%d", demoServePort)
		srv := &http.Server{
			Addr:              addr,
			Handler:           demo.Handler(cmd.OutOrStdout(), time.Now),
			ReadHeaderTimeout: 10 * time.Second,
		}

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.ErrOrStderr(), "listening on http://localhost:%d — agent replies will print below\n", demoServePort)
		fmt.Fprintf(cmd.ErrOrStderr(), "deploy with this as your endpoint (via a public tunnel for the hosted backend):\n")
		fmt.Fprintf(cmd.ErrOrStderr(), "  chariot deploy --count N --endpoint https://<your-tunnel>/chariot\n\n")

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		errc := make(chan error, 1)
		go func() { errc <- srv.Serve(ln) }()

		select {
		case err := <-errc:
			return err
		case <-ctx.Done():
			fmt.Fprintln(cmd.ErrOrStderr(), "\nshutting down")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		}
	},
}

func init() {
	demoServeCmd.Flags().IntVar(&demoServePort, "port", 8787, "port to listen on")
	demoCmd.AddCommand(demoServeCmd)
}
