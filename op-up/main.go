package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/ethereum-optimism/optimism/op-chain-ops/devkeys"
	"github.com/ethereum-optimism/optimism/op-devstack/devtest"
	"github.com/ethereum-optimism/optimism/op-devstack/shim"
	"github.com/ethereum-optimism/optimism/op-devstack/stack"
	"github.com/ethereum-optimism/optimism/op-devstack/stack/match"
	"github.com/ethereum-optimism/optimism/op-devstack/sysgo"
	"github.com/ethereum-optimism/optimism/op-devstack/sysgo/inventory"
	"github.com/ethereum-optimism/optimism/op-devstack/sysgo/manifest"
	opservice "github.com/ethereum-optimism/optimism/op-service"
	"github.com/ethereum-optimism/optimism/op-service/cliapp"
	"github.com/ethereum-optimism/optimism/op-service/client"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	oplog "github.com/ethereum-optimism/optimism/op-service/log"
	"github.com/ethereum-optimism/optimism/op-service/log/logfilter"
	"github.com/ethereum-optimism/optimism/op-service/testreq"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/trace"
)

const asciiArt = ` ____  ____        _     ____
/  _ \/  __\      / \ /\/  __\
| / \||  \/|_____ | | |||  \/|
| \_/||  __/\____\| \_/||  __/
\____/\_/         \____/\_/`

var (
	Version     = "v0.0.0"
	VersionMeta = "dev"
	GitCommit   string
	GitDate     string

	envPrefix = "OP_UP"
	dirFlag   = &cli.PathFlag{
		Name:    "dir",
		Usage:   "the path to the op-up directory, which is used for caching among other things.",
		EnvVars: opservice.PrefixEnvVar(envPrefix, "DIR"),
		Value: func() string {
			parentDir, err := os.UserHomeDir()
			if err != nil {
				parentDir, err = os.Getwd()
				if err != nil {
					return "error: could not find home or working directories"
				}
			}
			return filepath.Join(parentDir, ".op-up")
		}(),
	}
	configsFlag = &cli.StringSliceFlag{
		Name:      "config",
		Usage:     "paths to the inventory.yaml and manifest.yaml files.",
		EnvVars:   opservice.PrefixEnvVar(envPrefix, "CONFIG"),
		TakesFile: true,
		Action: func(_ *cli.Context, configPaths []string) error {
			switch len(configPaths) {
			case 0, 2:
				return nil
			default:
				return fmt.Errorf("expected 0 or 2 config paths, got %d", len(configPaths))
			}
		},
	}
)

type Config struct {
	DataDir   string
	Inventory inventory.Inventory
	Manifest  manifest.Manifest
}

func newConfigFromCLI(cliCtx *cli.Context) (*Config, error) {
	configPaths := cliCtx.StringSlice(configsFlag.Name)
	var inv *inventory.Inventory
	var manif *manifest.Manifest
	switch len(configPaths) {
	case 0:
		inv = &inventory.Inventory{
			Chains: []inventory.Chain{
				{
					Nodes: []inventory.Node{
						{
							Kind: "node",
							Name: "sequencer-0",
							Spec: inventory.NodeSpec{
								Kind: "sequencer",
								EL: inventory.Service{
									Kind: "op-geth",
								},
								CL: inventory.Service{
									Kind: "op-node",
								},
							},
						},
					},
					Services: []inventory.Service{
						{
							Kind: "proposer",
							Name: "op-proposer",
							Deps: inventory.Dependencies{
								Nodes: []string{"sequencer-0"},
							},
						},
						{
							Kind: "batcher",
							Name: "op-batcher",
							Spec: inventory.ServiceSpec{
								Kind: "op-batcher",
							},
							Deps: inventory.Dependencies{
								Nodes: []string{"sequencer-0"},
							},
						},
						{
							Kind: "faucet",
							Name: "op-faucet",
							Spec: inventory.ServiceSpec{
								Kind: "op-faucet",
							},
							Deps: inventory.Dependencies{
								Nodes: []string{"sequencer-0"},
							},
						},
					},
				},
			},
			Services: []inventory.Service{},
		}
		manif = &manifest.Manifest{
			Name: "",
			Type: "",
			L1: manifest.L1Config{
				ChainID: 101010,
				Name:    "testl1",
			},
			L2: manifest.L2Config{
				Chains: []manifest.Chain{
					{
						Name: "testl2",
						Id:   sysgo.DefaultL2AID.String(),
					},
				},
			},
		}
	case 2:
		var err error
		inv, err = inventory.NewInventoryFromPath(configPaths[0])
		if err != nil {
			return nil, fmt.Errorf("new inventory from path: %v", err)
		}
		manif, err = manifest.NewManifestFromPath(configPaths[1])
		if err != nil {
			return nil, fmt.Errorf("new manifest from path: %v", err)
		}
	default:
		return nil, fmt.Errorf("this should not happen: expected 0 or 2 config paths, got %d", len(configPaths))
	}
	return &Config{
		DataDir:   cliCtx.String(dirFlag.Name),
		Inventory: *inv,
		Manifest:  *manif,
	}, nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	app := cli.NewApp()
	app.Writer = stdout
	app.ErrWriter = stderr
	app.Version = opservice.FormatVersion(Version, GitCommit, GitDate, VersionMeta)
	app.Name = "op-up"
	app.Usage = "deploys an in-memory OP Stack devnet."
	app.Flags = cliapp.ProtectFlags([]cli.Flag{dirFlag, configsFlag})
	// The default OnUsageError behavior will print the error twice: once in the cli package and
	// once in our main function.
	// The function below prints help and returns the error for further handling/error messages.
	app.OnUsageError = func(cliCtx *cli.Context, err error, isSubcommand bool) error {
		if !cliCtx.App.HideHelp {
			_ = cli.ShowAppHelp(cliCtx)
		}
		return err
	}
	app.Action = func(cliCtx *cli.Context) error {
		config, err := newConfigFromCLI(cliCtx)
		if err != nil {
			return err
		}
		return runOpUp(cliCtx.Context, cliCtx.App.ErrWriter, config)
	}
	return app.RunContext(ctx, args)
}

func runOpUp(ctx context.Context, stderr io.Writer, config *Config) error {
	fmt.Fprintf(stderr, "%s\n", asciiArt)

	if err := os.MkdirAll(config.DataDir, 0o755); err != nil {
		return fmt.Errorf("create the op-up dir: %w", err)
	}
	deployerCacheDir := filepath.Join(config.DataDir, "deployer", "cache")
	if err := os.MkdirAll(deployerCacheDir, 0o755); err != nil {
		return fmt.Errorf("create the deployer cache dir: %w", err)
	}

	devtest.RootContext = ctx

	p := newP(ctx, stderr)
	defer p.Close()

	l1ChainID := eth.ChainIDFromUInt64(config.Manifest.L1.ChainID)
	l1ELID := stack.NewL1ELNodeID("l1", l1ChainID)
	l1CLID := stack.NewL1CLNodeID("l1", l1ChainID)
	opts := stack.Combine(
		sysgo.WithMnemonicKeys(devkeys.TestMnemonic),
		sysgo.WithDeployer(),
		sysgo.WithDeployerOptions(
			sysgo.WithEmbeddedContractSources(),
			sysgo.WithCommons(l1ChainID),
		),
		sysgo.WithDeployerPipelineOption(sysgo.WithDeployerCacheDir(deployerCacheDir)),
		sysgo.WithL1Nodes(l1ELID, l1CLID),
		sysgo.WithInventoryAndManifest(&config.Inventory, &config.Manifest, l1ELID, l1CLID),
	)

	orch := sysgo.NewOrchestrator(p, opts)
	stack.ApplyOptionLifecycle[*sysgo.Orchestrator](opts, orch)
	if err := runSysgo(ctx, stderr, orch); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "\nPlease consider filling out this survey to influence future development: https://www.surveymonkey.com/r/JTGHFK3\n")
	return nil
}

func newP(ctx context.Context, stderr io.Writer) devtest.P {
	logHandler := oplog.NewLogHandler(stderr, oplog.DefaultCLIConfig())
	logHandler = logfilter.WrapFilterHandler(logHandler)
	logHandler.(logfilter.FilterHandler).Set(logfilter.DefaultMute())
	logHandler = logfilter.WrapContextHandler(logHandler)
	logger := log.NewLogger(logHandler)
	oplog.SetGlobalLogHandler(logHandler)
	logger.SetContext(ctx)
	onFail := func(now bool) {
		logger.Error("Main failed")
		debug.PrintStack()
		if now {
			panic("critical Main fail")
		}
	}
	p := devtest.NewP(ctx, logger, onFail, func() {
		onFail(true)
	})
	return p
}

func runSysgo(ctx context.Context, stderr io.Writer, orch *sysgo.Orchestrator) error {
	// Print available account.
	hd, err := devkeys.NewMnemonicDevKeys(devkeys.TestMnemonic)
	if err != nil {
		return fmt.Errorf("new mnemonic dev keys: %w", err)
	}
	const funderIndex = 10_000 // see sysgo/deployer.go.
	funderUserKey := devkeys.UserKey(funderIndex)
	funderAddress, err := hd.Address(funderUserKey)
	if err != nil {
		return fmt.Errorf("address: %w", err)
	}
	funderPrivKey, err := hd.Secret(funderUserKey)
	if err != nil {
		return fmt.Errorf("secret: %w", err)
	}

	fmt.Fprintf(stderr, "Test Account Address: %s\n", funderAddress)
	fmt.Fprintf(stderr, "Test Account Private Key: %s\n", "0x"+common.Bytes2Hex(crypto.FromECDSA(funderPrivKey)))
	fmt.Fprintf(stderr, "EL Node URL: %s\n", "http://localhost:8545")

	t := &testingT{
		ctx:      ctx,
		cleanups: make([]func(), 0),
	}
	defer t.doCleanup()
	sys := shim.NewSystem(t)
	orch.Hydrate(sys)
	l2Networks := sys.L2Networks()
	if len(l2Networks) == 0 {
		return errors.New("no l2 networks found")
	}
	l2Net := l2Networks[0]
	elNode := l2Net.L2ELNode(match.FirstL2EL)

	var wg sync.WaitGroup
	defer wg.Wait()

	// Log on new blocks.
	wg.Add(1)
	go func() {
		defer wg.Done()
		const blockPollInterval = 500 * time.Millisecond
		var lastBlock uint64
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(blockPollInterval):
				unsafe, err := elNode.EthClient().BlockRefByLabel(ctx, eth.Unsafe)
				if err != nil {
					continue
				}
				if unsafe.Number != lastBlock {
					fmt.Fprintf(stderr, "New L2 block: number %d, hash %s\n", unsafe.Number, unsafe.Hash)
					lastBlock = unsafe.Number
				}
			}
		}
	}()

	// Proxy L2 EL requests.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := proxyEL(ctx, stderr, elNode.L2EthClient().RPC()); err != nil {
			fmt.Fprintf(stderr, "error: %v", err)
		}
	}()

	return nil
}

// proxyEL is a hacky way to intercept EL json rpc requests for logging to get around log filtering
// bugs.
func proxyEL(ctx context.Context, stderr io.Writer, client client.RPC) error {
	// Set up the HTTP handler for all incoming requests.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Ensure the request method is POST, as JSON RPC typically uses POST.
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read the entire request body.
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
			return
		}
		defer r.Body.Close() // Close the request body after reading

		// Parse the incoming JSON RPC request. We use a map to dynamically
		// extract the method, parameters, and ID.
		var req map[string]any
		if err := json.Unmarshal(requestBody, &req); err != nil {
			http.Error(w, "Invalid JSON RPC request format", http.StatusBadRequest)
			return
		}

		// Extract the RPC method name.
		method, ok := req["method"].(string)
		if !ok {
			http.Error(w, "Missing or invalid 'method' field in JSON RPC request", http.StatusBadRequest)
			return
		}

		// Extract RPC parameters. JSON RPC parameters can be an array, an object, or null/missing.
		var callParams []any
		if p, ok := req["params"]; ok && p != nil {
			if arr, isArray := p.([]any); isArray {
				// If parameters are an array, spread them directly.
				callParams = arr
			} else if obj, isObject := p.(map[string]any); isObject {
				// If parameters are a JSON object, pass the entire object as a single argument.
				callParams = []any{obj}
			} else {
				http.Error(w, "Invalid 'params' field in JSON RPC request (must be array, object, or null)", http.StatusBadRequest)
				return
			}
		}
		// If 'params' is missing or null, `callParams` remains empty, which is correct for methods without parameters.

		// Extract the request ID. This is crucial for matching responses to requests.
		id := req["id"] // ID can be string, number, or null. We don't need to check `ok` for this.

		// Prepare a variable to hold the RPC response result.
		// `json.RawMessage` is used to capture the raw JSON value from the backend
		// without needing to know its specific Go type beforehand.
		var rpcResult json.RawMessage

		fmt.Fprintf(stderr, "%s\n", method)

		// Use the rpc.Client to make the actual call to the backend Ethereum node.
		// The `callParams...` syntax unpacks the slice into variadic arguments.
		if err = client.CallContext(ctx, &rpcResult, method, callParams...); err != nil {
			message := fmt.Sprintf("RPC call to backend failed for method '%s': %v", method, err)
			// If the RPC call to the backend fails, construct a JSON RPC error response.
			rpcErr := map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"error": map[string]any{
					"code":    -32000, // Standard JSON RPC server error code for internal errors
					"message": message,
				},
			}
			fmt.Fprintf(stderr, "RPC error: %s\n", message)
			jsonResponse, _ := json.Marshal(rpcErr) // Marshaling error is unlikely here, so we ignore it.
			w.Header().Set("Content-Type", "application/json")
			// For JSON-RPC, errors are typically returned with an HTTP 200 OK status,
			// with the error details within the JSON payload.
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write(jsonResponse); err != nil {
				return
			}
			return
		}

		// If the RPC call was successful, construct the JSON RPC success response.
		responseMap := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  rpcResult, // The raw JSON result from the backend node
		}

		jsonResponse, err := json.Marshal(responseMap)
		if err != nil {
			http.Error(w, "Failed to marshal RPC success response", http.StatusInternalServerError)
			return
		}

		// Set the Content-Type header and write the successful JSON RPC response.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(jsonResponse); err != nil {
			return
		}
	})

	srv := &http.Server{
		Addr:    "localhost:8545",
		Handler: mux,
	}

	var wg sync.WaitGroup
	defer wg.Wait()
	errCh := make(chan error)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(errCh)
		// Start the HTTP server.
		if err := srv.ListenAndServe(); err != nil {
			errCh <- fmt.Errorf("listen and serve: %w", err)
		}
	}()
	<-ctx.Done()
	if err := srv.Shutdown(context.Background()); err != nil {
		return fmt.Errorf("shut down server: %w", err)
	}
	return <-errCh
}

type testingT struct {
	mu       sync.Mutex
	ctx      context.Context
	cleanups []func()
}

var _ devtest.T = (*testingT)(nil)
var _ testreq.TestingT = (*testingT)(nil)

func (t *testingT) doCleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, cleanup := range slices.Backward(t.cleanups) {
		cleanup()
	}
}

// Cleanup implements devtest.T.
func (t *testingT) Cleanup(fn func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cleanups = append(t.cleanups, fn)
}

// Ctx implements devtest.T.
func (t *testingT) Ctx() context.Context {
	return t.ctx
}

// Deadline implements devtest.T.
func (t *testingT) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}

// Error implements devtest.T.
func (t *testingT) Error(args ...any) {
}

// Errorf implements devtest.T.
func (t *testingT) Errorf(format string, args ...any) {
}

// Fail implements devtest.T.
func (t *testingT) Fail() {
}

// FailNow implements devtest.T.
func (t *testingT) FailNow() {
}

// Gate implements devtest.T.
func (t *testingT) Gate() *testreq.Assertions {
	return testreq.New(t)
}

// Helper implements devtest.T.
func (t *testingT) Helper() {
}

// Log implements devtest.T.
func (t *testingT) Log(args ...any) {
}

// Logf implements devtest.T.
func (t *testingT) Logf(format string, args ...any) {
}

func (t *testingT) Logger() log.Logger {
	return log.NewLogger(slog.NewTextHandler(io.Discard, nil))
}

func (t *testingT) Name() string {
	return "dev"
}

func (t *testingT) Parallel() {
}

func (t *testingT) Require() *testreq.Assertions {
	return testreq.New(t)
}

func (t *testingT) Run(name string, fn func(devtest.T)) {
	panic("unimplemented")
}

func (t *testingT) Skip(args ...any) {
	panic("unimplemented")
}

func (t *testingT) SkipNow() {
	panic("unimplemented")
}

// Skipf implements devtest.T.
func (t *testingT) Skipf(format string, args ...any) {
	panic("unimplemented")
}

// Skipped implements devtest.T.
func (t *testingT) Skipped() bool {
	return false
}

// TempDir implements devtest.T.
func (t *testingT) TempDir() string {
	panic("unimplemented")
}

// Tracer implements devtest.T.
func (t *testingT) Tracer() trace.Tracer {
	panic("unimplemented")
}

// WithCtx implements devtest.T.
func (t *testingT) WithCtx(ctx context.Context) devtest.T {
	return t
}

// _TestOnly implements devtest.T.
func (t *testingT) TestOnly() {
}
