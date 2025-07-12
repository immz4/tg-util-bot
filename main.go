package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	tele "gopkg.in/telebot.v4"
)

const (
	DefaultPollTimeout = 10 * time.Second
)

type ResendTarget string

func (id ResendTarget) Recipient() string {
	return string(id)
}

type Command struct {
	Text        string `koanf:"text" validate:"required_if=Enabled true"`
	Description string `koanf:"description" validate:"required_if=Enabled true"`
}

type ResendConfig struct {
	Enabled  bool     `koanf:"enabled"`
	Command  Command  `koanf:"command" validate:"required_if=Enabled true"`
	Keywords []string `koanf:"keywords"`
	From     []int64  `koanf:"from" validate:"required_if=Enabled true"`
	To       []int64  `koanf:"to" validate:"required_if=Enabled true"`
}

type FeedbackConfig struct {
	Enabled bool    `koanf:"enabled"`
	To      []int64 `koanf:"to" validate:"required_if=Enabled true"`
}

type Config struct {
	Token    string         `koanf:"token" validate:"required,len=46"`
	Port     string         `koanf:"port" validate:"required"`
	Resend   ResendConfig   `koanf:"resend"`
	Feedback FeedbackConfig `koanf:"feedback"`
}

type App struct {
	Config    Config
	Bot       *tele.Bot
	Validator *validator.Validate
	Logger    *log.Logger
}

func NewApp(configData string) (*App, error) {
	app := &App{
		Validator: validator.New(validator.WithRequiredStructEnabled()),
		Logger:    log.New(os.Stdout, "[TG-BOT] ", log.LstdFlags),
	}

	if err := app.loadConfig(configData); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if err := app.initBot(); err != nil {
		return nil, fmt.Errorf("failed to initialize bot: %w", err)
	}

	app.setupHandlers()
	return app, nil
}

func (a *App) loadConfig(configData string) error {
	k := koanf.New(".")

	if err := k.Load(rawbytes.Provider([]byte(configData)), toml.Parser()); err != nil {
		return fmt.Errorf("error parsing config: %w", err)
	}

	if err := k.Unmarshal("", &a.Config); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}

	if err := a.Validator.Struct(a.Config); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	return nil
}

func (a *App) initBot() error {
	settings := tele.Settings{
		Token:  a.Config.Token,
		Poller: &tele.LongPoller{Timeout: DefaultPollTimeout},
	}

	bot, err := tele.NewBot(settings)
	if err != nil {
		return fmt.Errorf("failed to create bot: %w", err)
	}

	a.Bot = bot
	return nil
}

func (a *App) setupHandlers() {
	var commands []tele.Command

	a.Bot.Handle("/id", a.handleIDCommand)
	commands = append(commands, tele.Command{
		Text:        "/id",
		Description: "Get ID of this chat",
	})

	if a.Config.Resend.Enabled {
		a.Logger.Println("Enabling resend functionality")
		a.setupResendHandlers()
		commands = append(commands, tele.Command{
			Text:        a.Config.Resend.Command.Text,
			Description: a.Config.Resend.Command.Description,
		})
	}

	if a.Config.Feedback.Enabled {
		a.Logger.Println("Enabling feedback functionality")
		a.setupFeedbackHandlers()
	}

	if err := a.Bot.SetCommands(commands); err != nil {
		a.Logger.Printf("Failed to set commands: %v", err)
	}
}

func (a *App) handleIDCommand(c tele.Context) error {
	chatID := c.Chat().ID
	a.Logger.Printf("Received /id command from chat %d", chatID)
	return c.Send(strconv.FormatInt(chatID, 10))
}

func (a *App) setupResendHandlers() {
	handler := a.createResendHandler()

	a.Bot.Handle(a.Config.Resend.Command.Text, handler)

	for _, keyword := range a.Config.Resend.Keywords {
		a.Bot.Handle(keyword, handler)
	}
}

func (a *App) createResendHandler() tele.HandlerFunc {
	return func(c tele.Context) error {
		chatID := c.Chat().ID
		a.Logger.Printf("Received resend request from chat %d", chatID)

		if !c.Message().IsReply() || !slices.Contains(a.Config.Resend.From, chatID) {
			return nil
		}

		for _, targetID := range a.Config.Resend.To {
			target := ResendTarget(strconv.FormatInt(targetID, 10))
			a.Logger.Printf("Forwarding message from chat %d to %s", chatID, target)

			if _, err := a.Bot.Forward(target, c.Message().ReplyTo); err != nil {
				a.Logger.Printf("Failed to forward message to %s: %v", target, err)
			}
		}

		return nil
	}
}

func (a *App) setupFeedbackHandlers() {
	handler := a.createFeedbackHandler()
	a.Bot.Handle(tele.OnPhoto, handler)
	a.Bot.Handle(tele.OnText, handler)
}

func (a *App) createFeedbackHandler() tele.HandlerFunc {
	return func(c tele.Context) error {
		if c.Chat().Type != tele.ChatPrivate {
			return nil
		}

		chatID := c.Chat().ID
		a.Logger.Printf("Received feedback from private chat %d", chatID)

		for _, targetID := range a.Config.Feedback.To {
			target := ResendTarget(strconv.FormatInt(targetID, 10))
			a.Logger.Printf("Forwarding feedback from chat %d to %s", chatID, target)

			if _, err := a.Bot.Forward(target, c.Message()); err != nil {
				a.Logger.Printf("Failed to forward feedback to %s: %v", target, err)
			}
		}

		return nil
	}
}

func (a *App) startHTTPServer(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	port := a.Config.Port

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	a.Logger.Printf("Starting HTTP server on port %s", port)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Logger.Printf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	a.Logger.Println("Shutting down HTTP server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}

func (a *App) startTelegramBot(ctx context.Context) error {
	a.Logger.Println("Starting Telegram bot...")

	go a.Bot.Start()

	<-ctx.Done()
	a.Logger.Println("Stopping Telegram bot...")
	a.Bot.Stop()

	return nil
}

func (a *App) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.startHTTPServer(ctx); err != nil {
			errChan <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.startTelegramBot(ctx); err != nil {
			errChan <- fmt.Errorf("telegram bot error: %w", err)
		}
	}()

	select {
	case sig := <-sigChan:
		a.Logger.Printf("Received signal %v, shutting down...", sig)
		cancel()
	case err := <-errChan:
		a.Logger.Printf("Error occurred: %v", err)
		cancel()
		return err
	}

	wg.Wait()
	a.Logger.Println("Application stopped successfully")
	return nil
}

func main() {
	configData := os.Getenv("APP_CONFIG")
	if configData == "" {
		log.Fatal("APP_CONFIG env variable is required")
	}

	app, err := NewApp(configData)
	if err != nil {
		log.Fatalf("failed to create application: %v", err)
	}

	if err := app.Run(); err != nil {
		log.Fatalf("Application error: %v", err)
	}
}
