package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/telegram"
	"github.com/protonspy/csdd/internal/workspace"
)

// apiBase lets TELEGRAM_API_BASE override the public Bot API endpoint — useful
// behind a corporate proxy, against a self-hosted local Bot API server, or for
// tests. Empty (the default) means the public api.telegram.org.
func apiBase() string { return strings.TrimSpace(os.Getenv("TELEGRAM_API_BASE")) }

// runTelegram dispatches `csdd telegram {init,run}`: a read-only, outbound-only
// bridge that relays spec-status changes and plan journals to a Telegram
// chat. Like `csdd web`, it never authors artifacts — it is a view over the
// workspace, delivered as messages.
func runTelegram(args []string) int {
	if len(args) == 0 {
		render.Err("usage: " + prog() + " telegram {init,run}")
		return 1
	}
	switch args[0] {
	case "init":
		return telegramInit(args[1:])
	case "run":
		return telegramRun(args[1:])
	case "-h", "--help", "help":
		fmt.Println("usage: " + prog() + " telegram {init,run}\n" +
			"  init   Save the bot token + chat_id to .csdd/bot.json (gitignored) and send a test message.\n" +
			"  run    Relay plan-run progress and spec-status changes to the configured chat (blocks; Ctrl-C to stop).")
		return 0
	default:
		render.Err("unknown telegram action: " + args[0])
		return 1
	}
}

// telegramInit writes .csdd/bot.json from flags/env or an interactive prompt,
// gitignores it (the token is a secret), and — unless --no-test — sends a
// confirmation message to prove the token/chat work.
func telegramInit(args []string) int {
	fs := flag.NewFlagSet("telegram init", flag.ContinueOnError)
	var root, token, chatID string
	var interval int
	var noTest bool
	addRoot(fs, &root)
	fs.StringVar(&token, "token", os.Getenv("TELEGRAM_BOT_TOKEN"), "Bot token from @BotFather (default: TELEGRAM_BOT_TOKEN env).")
	fs.StringVar(&chatID, "chat-id", os.Getenv("TELEGRAM_CHAT_ID"), "Destination chat id or @username (default: TELEGRAM_CHAT_ID env).")
	fs.IntVar(&interval, "interval", 0, "Poll interval in seconds for `telegram run` (0 = default 5s).")
	fs.BoolVar(&noTest, "no-test", false, "Do not send a confirmation message after saving.")
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}

	// A single reader for the whole prompt flow, so a multi-line answer isn't
	// swallowed by a throwaway bufio buffer between questions.
	var in io.Reader = os.Stdin
	if confirmStdin != nil {
		in = confirmStdin
	}
	br := bufio.NewReader(in)
	ask := func(q string) string {
		if !stdinIsInteractive() {
			return ""
		}
		render.Ask(q)
		line, _ := br.ReadString('\n')
		return strings.TrimSpace(line)
	}
	if token == "" {
		token = ask("Telegram bot token (@BotFather): ")
	}
	if chatID == "" {
		chatID = ask("Destination chat_id (or @username): ")
	}

	cfg := telegram.Config{Token: strings.TrimSpace(token), ChatID: strings.TrimSpace(chatID), IntervalSeconds: interval}
	if err := cfg.Validate(); err != nil {
		render.Err(err.Error() + " — pass --token/--chat-id, set TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID, or run interactively")
		return 1
	}
	if err := telegram.Save(r, cfg); err != nil {
		render.Err("could not save config: " + err.Error())
		return 1
	}
	if _, err := ensureGitignore(r, []string{".csdd/bot.json"}); err != nil {
		render.Warn("could not gitignore .csdd/bot.json (holds a secret token): " + err.Error())
	}
	render.OK("saved telegram config to " + telegram.ConfigFile + " under .csdd/ (gitignored)")

	if noTest {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := telegram.NewClient(cfg.Token, cfg.ChatID, apiBase())
	if err := client.Send(ctx, "✅ csdd telegram configurado — notificações ativas."); err != nil {
		render.Warn("config saved, but the test message failed: " + err.Error())
		render.Info("check the token/chat_id, or re-run `" + prog() + " telegram init`")
		return 1
	}
	render.OK("test message delivered — run `" + prog() + " telegram run` to start relaying")
	return 0
}

// telegramRun loads the saved config and relays updates until interrupted.
func telegramRun(args []string) int {
	fs := flag.NewFlagSet("telegram run", flag.ContinueOnError)
	var root string
	var interval int
	addRoot(fs, &root)
	fs.IntVar(&interval, "interval", 0, "Poll interval in seconds (0 = config value, else default 5s).")
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	cfg, err := telegram.Load(r)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	if err := cfg.Validate(); err != nil {
		render.Err(err.Error() + " — re-run `" + prog() + " telegram init`")
		return 1
	}

	seconds := interval
	if seconds == 0 {
		seconds = cfg.IntervalSeconds
	}
	notifier := telegram.NewNotifier(telegram.Options{
		Root:     r,
		Client:   telegram.NewClient(cfg.Token, cfg.ChatID, apiBase()),
		Interval: time.Duration(seconds) * time.Second,
		Out:      os.Stdout,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	render.Info("csdd telegram notifier — relaying plan-run + spec-status to chat " + cfg.ChatID + " (Ctrl-C to stop)")
	if err := notifier.Run(ctx); err != nil {
		render.Err(err.Error())
		return 1
	}
	render.Info("telegram notifier stopped")
	return 0
}
