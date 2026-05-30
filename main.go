package main

import (
        "fmt"
        "log"
        "os"
        "os/signal"
        "syscall"
        "time"

        "github.com/teacat/chaturbate-dvr/config"
        "github.com/teacat/chaturbate-dvr/entity"
        "github.com/teacat/chaturbate-dvr/manager"
        "github.com/teacat/chaturbate-dvr/router"
        "github.com/teacat/chaturbate-dvr/server"
        "github.com/urfave/cli/v2"
)

const logo = `
 ██████╗██╗  ██╗ █████╗ ████████╗██╗   ██╗██████╗ ██████╗  █████╗ ████████╗███████╗
██╔════╝██║  ██║██╔══██╗╚══██╔══╝██║   ██║██╔══██╗██╔══██╗██╔══██╗╚══██╔══╝██╔════╝
██║     ███████║███████║   ██║   ██║   ██║██████╔╝██████╔╝███████║   ██║   █████╗
██║     ██╔══██║██╔══██║   ██║   ██║   ██║██╔══██╗██╔══██╗██╔══██║   ██║   ██╔══╝
╚██████╗██║  ██║██║  ██║   ██║   ╚██████╔╝██║  ██║██████╔╝██║  ██║   ██║   ███████╗
 ╚═════╝╚═╝  ╚═╝╚═╝  ╚═╝   ╚═╝    ╚═════╝ ╚═╝  ╚═╝╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚══════╝
██████╗ ██╗   ██╗██████╗
██╔══██╗██║   ██║██╔══██╗
██║  ██║██║   ██║██████╔╝
██║  ██║╚██╗ ██╔╝██╔══██╗
██████╔╝ ╚████╔╝ ██║  ██║
╚═════╝   ╚═══╝  ╚═╝  ╚═╝`

var version = "dev"

func main() {
        app := &cli.App{
                Name:    "chaturbate-dvr",
                Version: version,
                Usage:   "Record your favorite Chaturbate streams automatically. 😎🫵",
                Flags: []cli.Flag{
                        &cli.StringFlag{
                                Name:    "username",
                                Aliases: []string{"u"},
                                Usage:   "The username of the channel to record",
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:  "admin-username",
                                Usage: "Username for web authentication (optional)",
                                Value: "",
                        },
                        &cli.StringFlag{
                                Name:  "admin-password",
                                Usage: "Password for web authentication (optional)",
                                Value: "",
                        },
                        &cli.IntFlag{
                                Name:  "framerate",
                                Usage: "Desired framerate (FPS)",
                                Value: 30,
                        },
                        &cli.IntFlag{
                                Name:  "resolution",
                                Usage: "Desired resolution (e.g., 1080 for 1080p)",
                                Value: 1080,
                        },
                        &cli.StringFlag{
                                Name:  "pattern",
                                Usage: "Template for naming recorded videos",
                                Value: "videos/{{.Username}}_{{.Year}}-{{.Month}}-{{.Day}}_{{.Hour}}-{{.Minute}}-{{.Second}}{{if .Sequence}}_{{.Sequence}}{{end}}",
                        },
                        &cli.IntFlag{
                                Name:  "max-duration",
                                Usage: "Split video into segments every N minutes ('0' to disable)",
                                Value: 0,
                        },
                        &cli.IntFlag{
                                Name:  "max-filesize",
                                Usage: "Split video into segments every N MB ('0' to disable)",
                                Value: 0,
                        },
                        &cli.StringFlag{
                                Name:    "port",
                                Aliases: []string{"p"},
                                Usage:   "Port for the web interface and API",
                                Value:   "8080",
                        },
                        &cli.IntFlag{
                                Name:  "interval",
                                Usage: "Check if the channel is online every N minutes",
                                Value: 1,
                        },
                        &cli.StringFlag{
                                Name:    "cookies",
                                Usage:   "Cookies to use in the request (format: key=value; key2=value2)",
                                EnvVars: []string{"COOKIES"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "user-agent",
                                Usage:   "Custom User-Agent for the request",
                                EnvVars: []string{"USER_AGENT"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:  "domain",
                                Usage: "Chaturbate domain to use",
                                Value: "https://chaturbate.com/",
                        },
                        &cli.BoolFlag{
                                Name:  "compress",
                                Usage: "Compress recorded files (.ts or .mp4) to .mkv using ffmpeg after recording (auto-enabled if ffmpeg is installed)",
                                Value: false,
                        },
                        &cli.StringFlag{
                                Name:    "output-dir",
                                Usage:   "Directory to move completed recordings to (empty = keep in place)",
                                EnvVars: []string{"OUTPUT_DIR"},
                                Value:   "",
                        },
                        &cli.BoolFlag{
                                Name:    "per-model-folder",
                                Usage:   "Create a subdirectory per model inside --output-dir",
                                EnvVars: []string{"PER_MODEL_FOLDER"},
                                Value:   false,
                        },
                        &cli.BoolFlag{
                                Name:    "delete-local-after-upload",
                                Usage:   "Delete local recordings and preview files after successful remote upload",
                                EnvVars: []string{"DELETE_LOCAL_AFTER_UPLOAD"},
                                Value:   true,
                        },
                        &cli.StringFlag{
                                Name:    "turboviplay-api-key",
                                Usage:   "API key for TurboViPlay uploads",
                                EnvVars: []string{"TURBOVIPLAY_API_KEY"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "voesx-api-key",
                                Usage:   "API key for VOE.sx uploads",
                                EnvVars: []string{"VOESX_API_KEY"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "sendcm-api-key",
                                Usage:   "API key for SendCM uploads (optional, guest upload if empty)",
                                EnvVars: []string{"SENDCM_API_KEY"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "byse-api-key",
                                Usage:   "API key for Byse uploads",
                                EnvVars: []string{"BYSE_API_KEY"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "streamtape-login",
                                Usage:   "Login username for Streamtape uploads",
                                EnvVars: []string{"STREAMTAPE_LOGIN"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "streamtape-key",
                                Usage:   "API key for Streamtape uploads",
                                EnvVars: []string{"STREAMTAPE_KEY"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "mixdrop-email",
                                Usage:   "Email for Mixdrop uploads",
                                EnvVars: []string{"MIXDROP_EMAIL"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "mixdrop-token",
                                Usage:   "API token for Mixdrop uploads",
                                EnvVars: []string{"MIXDROP_TOKEN", "MIXDROP_KEY"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "pixeldrain-token",
                                Usage:   "API token for PixelDrain uploads",
                                EnvVars: []string{"PIXELDRAIN_TOKEN", "PIXELDRAIN_API_KEY"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "github-token",
                                Usage:   "GitHub Personal Access Token for preview/sprite image uploads",
                                EnvVars: []string{"GITHUB_TOKEN"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "github-repo",
                                Usage:   "GitHub repository for preview images (owner/repo)",
                                EnvVars: []string{"GITHUB_REPO"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "github-branch",
                                Usage:   "GitHub branch for preview images (default: main)",
                                EnvVars: []string{"GITHUB_BRANCH"},
                                Value:   "main",
                        },
                        &cli.StringFlag{
                                Name:    "github-preview-path",
                                Usage:   "Path in GitHub repo for preview images (default: previews)",
                                EnvVars: []string{"GITHUB_PREVIEW_PATH"},
                                Value:   "previews",
                        },
                        &cli.StringFlag{
                                Name:    "flaresolverr-url",
                                Usage:   "URL of the Byparr/FlareSolverr instance for automatic Cloudflare bypass",
                                EnvVars: []string{"FLARESOLVERR_URL"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "supabase-url",
                                Usage:   "Supabase project URL for remote data persistence (REST API fallback)",
                                EnvVars: []string{"SUPABASE_URL"},
                                Value:   "",
                        },
                        &cli.StringFlag{
                                Name:    "supabase-api-key",
                                Usage:   "Supabase anon/public API key for REST API fallback",
                                EnvVars: []string{"SUPABASE_API_KEY"},
                                Value:   "",
                        },
                },
                Action: start,
        }
        if err := app.Run(os.Args); err != nil {
                log.Fatal(err)
        }
}

func start(c *cli.Context) error {
        fmt.Println(logo)

        var err error
        server.Config, err = config.New(c)
        if err != nil {
                return fmt.Errorf("new config: %w", err)
        }
        server.Manager, err = manager.New()
        if err != nil {
                return fmt.Errorf("new manager: %w", err)
        }

        // init web interface if username is not provided
        if server.Config.Username == "" {
                fmt.Printf("👋 Visit http://localhost:%s to use the Web UI\n\n\n", c.String("port"))

                if err := server.Manager.LoadConfig(); err != nil {
                        return fmt.Errorf("load config: %w", err)
                }

                // Start the built-in cookie-refresher (replicates the docker-compose
                // cookie-refresher container). It calls Byparr every 30 min to obtain
                // fresh cf_clearance cookies automatically.
                server.Manager.StartCookieRefresher()

                // Graceful shutdown: catch SIGTERM/SIGINT, stop all recording
                // channels first (so their Cleanup() runs and queues files into
                // UploadWg), then wait for post-processing + uploads + Supabase
                // saves to finish before exiting.  A progress ticker logs every
                // 30 s so the GitHub Actions log shows the process is still alive.
                go func() {
                        sigCh := make(chan os.Signal, 1)
                        signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
                        <-sigCh

                        channels := server.Manager.ChannelInfo()
                        fmt.Printf("\n[SHUTDOWN] received signal — stopping %d channel(s)...\n", len(channels))
                        for _, ch := range channels {
                                fmt.Printf("[SHUTDOWN]   stopping %s\n", ch.Username)
                        }

                        server.Manager.StopAllChannels()
                        fmt.Println("[SHUTDOWN] all channels stopped — waiting for mux/thumbnail/upload/Supabase to finish...")
                        fmt.Println("[SHUTDOWN] DO NOT kill this process — recordings will be lost if interrupted")

                        // Progress ticker: log every 30 s so CI logs show we are alive
                        shutdownDone := make(chan struct{})
                        go func() {
                                ticker := time.NewTicker(30 * time.Second)
                                defer ticker.Stop()
                                elapsed := 30
                                for {
                                        select {
                                        case <-ticker.C:
                                                fmt.Printf("[SHUTDOWN] still finalizing... (%ds elapsed — waiting for uploads and Supabase saves)\n", elapsed)
                                                elapsed += 30
                                        case <-shutdownDone:
                                                return
                                        }
                                }
                        }()

                        server.Manager.WaitForAllChannels()
                        fmt.Println("[SHUTDOWN] all recordings finalized — waiting for uploads and Supabase saves...")
                        server.Manager.WaitForUploads()

                        close(shutdownDone)
                        fmt.Println("[SHUTDOWN] all uploads and Supabase saves complete — exiting cleanly")
                        os.Exit(0)
                }()

                return router.SetupRouter().Run(":" + c.String("port"))
        }

        // else create a channel with the provided username
        if err := server.Manager.CreateChannel(&entity.ChannelConfig{
                Username:    c.String("username"),
                Framerate:   c.Int("framerate"),
                Resolution:  c.Int("resolution"),
                Pattern:     c.String("pattern"),
                MaxDuration: c.Int("max-duration"),
                MaxFilesize: c.Int("max-filesize"),
                Compress:    c.Bool("compress"),
        }, false); err != nil {
                return fmt.Errorf("create channel: %w", err)
        }

        // block forever
        select {}
}
