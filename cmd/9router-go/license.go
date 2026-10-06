package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"9router/proxy/internal/config"
	"9router/proxy/internal/entitlements"
	"9router/proxy/internal/updater"
)

const defaultLicenseControlPlaneURL = "https://9router-licensing-prototype.joselofarias.workers.dev"

// These values are intentionally public build metadata. Release jobs may
// override them with -ldflags "-X main.licenseBuildChannel=... -X
// main.licenseBuildID=... -X main.licenseProCapableUntil=..." without ever
// embedding a signing private key in the client.
var (
	licenseBuildChannel    = "beta"
	licenseBuildID         = ""
	licenseProCapableUntil = ""
)

func licenseCommand() *cli.Command {
	return &cli.Command{
		Name:    "license",
		Aliases: []string{"licencia"},
		Usage:   "Manage the local 9router license",
		Action:  licenseMenuAction,
		Subcommands: []*cli.Command{
			{
				Name:      "activate",
				Aliases:   []string{"activar"},
				Usage:     "Activate Beta Pro using an invitation code",
				ArgsUsage: "[activation-code]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "stdin",
						Usage: "read the activation code from stdin instead of the command line",
					},
				},
				Action: licenseActivateAction,
			},
			{
				Name:    "status",
				Aliases: []string{"estado"},
				Usage:   "Show the verified local license state",
				Action:  licenseStatusAction,
			},
			{
				Name:    "renew",
				Aliases: []string{"renovar"},
				Usage:   "Renew the current signed lease",
				Action:  licenseRenewAction,
			},
			{
				Name:   "menu",
				Usage:  "Open the simple interactive license menu",
				Action: licenseMenuAction,
			},
		},
	}
}

func licenseMenuAction(cCtx *cli.Context) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println()
		fmt.Println("9router - Licencia")
		fmt.Println("==================")
		fmt.Println("1) Activar Pro")
		fmt.Println("2) Ver estado")
		fmt.Println("3) Renovar licencia")
		fmt.Println("0) Volver")
		fmt.Print("> ")

		choice, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			fmt.Print("Codigo de activacion: ")
			code, readErr := reader.ReadString('\n')
			if readErr != nil && readErr != io.EOF {
				return readErr
			}
			if err := activateLicense(cCtx.Context, strings.TrimSpace(code)); err != nil {
				fmt.Printf("Activacion fallida: %v\n", err)
			}
		case "2":
			if err := showLicenseStatus(); err != nil {
				fmt.Printf("No se pudo leer la licencia: %v\n", err)
			}
		case "3":
			if err := renewLicense(cCtx.Context); err != nil {
				fmt.Printf("Renovacion fallida: %v\n", err)
			}
		case "0", "q", "quit", "salir":
			return nil
		default:
			if choice != "" {
				fmt.Println("Opcion no valida.")
			}
		}

		if err == io.EOF {
			return nil
		}
	}
}

func licenseActivateAction(cCtx *cli.Context) error {
	code, err := activationCodeInput(cCtx)
	if err != nil {
		return err
	}
	return activateLicense(cCtx.Context, code)
}

func activationCodeInput(cCtx *cli.Context) (string, error) {
	if cCtx.Args().Len() > 0 {
		code := strings.TrimSpace(cCtx.Args().First())
		if code == "" {
			return "", entitlements.ErrActivationCodeRequired
		}
		return code, nil
	}

	reader := bufio.NewReader(os.Stdin)
	if cCtx.Bool("stdin") {
		code, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		code = strings.TrimSpace(code)
		if code == "" {
			return "", entitlements.ErrActivationCodeRequired
		}
		return code, nil
	}

	fmt.Print("Codigo de activacion: ")
	code, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return "", entitlements.ErrActivationCodeRequired
	}
	return code, nil
}

func licenseStatusAction(*cli.Context) error {
	return showLicenseStatus()
}

func licenseRenewAction(cCtx *cli.Context) error {
	return renewLicense(cCtx.Context)
}

func activateLicense(ctx context.Context, code string) error {
	client, err := newLicenseClient()
	if err != nil {
		return err
	}
	result, err := client.Activate(ctx, code)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Licencia activada correctamente.")
	printLicenseStatus(entitlements.NewLeaseProvider(result.Evaluation).Status())
	if result.RenewalAfter > 0 {
		fmt.Printf("Renovacion sugerida en: %s\n", result.RenewalAfter.Round(time.Minute))
	}
	return nil
}

func renewLicense(ctx context.Context) error {
	client, err := newLicenseClient()
	if err != nil {
		return err
	}
	result, err := client.Renew(ctx)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Licencia renovada correctamente.")
	printLicenseStatus(entitlements.NewLeaseProvider(result.Evaluation).Status())
	if result.RenewalAfter > 0 {
		fmt.Printf("Proxima renovacion sugerida en: %s\n", result.RenewalAfter.Round(time.Minute))
	}
	return nil
}

func showLicenseStatus() error {
	keys, err := entitlements.StagingKeyRing()
	if err != nil {
		return fmt.Errorf("load staging keyring: %w", err)
	}
	build, err := currentLicenseBuildIdentity()
	if err != nil {
		return err
	}

	store := entitlements.NewRuntimeStore(config.ResolveDataDir())
	state, loadErr := store.LoadRuntime(entitlements.RuntimeOptions{
		Keys:  keys,
		Build: build,
		Now:   time.Now,
	})
	if state == nil || state.Provider == nil {
		if loadErr != nil {
			return loadErr
		}
		return fmt.Errorf("license runtime unavailable")
	}

	fmt.Println()
	printLicenseStatus(state.Provider.Status())
	if loadErr != nil {
		fmt.Printf("Motivo del fallback: %v\n", loadErr)
	}
	return nil
}

func printLicenseStatus(status entitlements.Status) {
	if status.Mode != "licensed" {
		fmt.Println("Estado: Community")
		fmt.Println("Funciones Pro: desactivadas")
		return
	}

	fmt.Printf("Estado: %s\n", strings.ToUpper(status.State))
	fmt.Printf("Plan: %s\n", status.Plan)
	fmt.Printf("Canal: %s\n", status.Channel)
	fmt.Printf("Licencia: %s\n", status.LicenseID)
	fmt.Printf("Instalacion: %s\n", status.InstallationID)
	if status.ExpiresAt != nil {
		fmt.Printf("Lease activo hasta: %s\n", status.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if status.GraceUntil != nil {
		fmt.Printf("Gracia hasta: %s\n", status.GraceUntil.UTC().Format(time.RFC3339))
	}
	if len(status.Features) > 0 {
		fmt.Println("Funciones:")
		for _, feature := range status.Features {
			fmt.Printf("  - %s\n", feature)
		}
	}
}

func newLicenseClient() (*entitlements.Client, error) {
	keys, err := entitlements.StagingKeyRing()
	if err != nil {
		return nil, fmt.Errorf("load staging keyring: %w", err)
	}
	build, err := currentLicenseBuildIdentity()
	if err != nil {
		return nil, err
	}
	transport, err := entitlements.NewHTTPTransport(currentLicenseControlPlaneURL(), nil)
	if err != nil {
		return nil, err
	}

	return entitlements.NewClient(entitlements.ClientOptions{
		Store:      entitlements.NewRuntimeStore(config.ResolveDataDir()),
		Transport:  transport,
		Keys:       keys,
		Build:      build,
		Platform:   runtime.GOOS,
		Arch:       runtime.GOARCH,
		AppVersion: updater.CurrentVersion,
	})
}

func currentLicenseControlPlaneURL() string {
	if value := strings.TrimSpace(os.Getenv("NINEROUTER_LICENSE_URL")); value != "" {
		return value
	}
	return defaultLicenseControlPlaneURL
}

func currentLicenseBuildIdentity() (entitlements.BuildIdentity, error) {
	build := entitlements.BuildIdentity{
		Channel: strings.TrimSpace(licenseBuildChannel),
		ID:      strings.TrimSpace(licenseBuildID),
	}
	if build.Channel == "" {
		return entitlements.BuildIdentity{}, fmt.Errorf("license build channel is empty")
	}

	if raw := strings.TrimSpace(licenseProCapableUntil); raw != "" {
		deadline, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return entitlements.BuildIdentity{}, fmt.Errorf("invalid Pro-capable build deadline: %w", err)
		}
		build.ProCapableUntil = deadline.UTC()
	}
	return build, nil
}
