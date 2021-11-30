package unoconvdoc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gotenberg/gotenberg/v7/pkg/gotenberg"
	"github.com/gotenberg/gotenberg/v7/pkg/modules/api"
	"go.uber.org/zap"
)

func init() {
	gotenberg.MustRegisterModule(UnoconvDoc{})
}

// ErrMalformedPageRanges happens if the page ranges option cannot be
// interpreted by LibreOffice.
var ErrMalformedPageRanges = errors.New("page ranges are malformed")

// UnoconvDoc is a module which provides an API to interact with unoconv.
type UnoconvDoc struct {
	binPath string
}

// Options gathers available options when converting a document to PDF.
type Options struct {
	// Landscape allows to change the orientation of the resulting PDF.
	// Optional.
	Landscape bool

	// PageRanges allows to select the pages to convert.
	// TODO: should prefer a method form PDFEngine.
	// Optional.
	PageRanges string
}

// API is an abstraction on top of unoconv.
//
// See https://github.com/unoconv/unoconv.
type API interface {
	DOC(ctx context.Context, logger *zap.Logger, inputPath, outputPath string, options Options) error
	Extensions() []string
}

// Provider is a module interface which exposes a method for creating an API
// for other modules.
//
//	func (m *YourModule) Provision(ctx *gotenberg.Context) error {
//		provider, _ := ctx.Module(new(unoconv.Provider))
//		uno, _      := provider.(unoconv.Provider).Unoconv()
//	}
// type Provider interface {
// 	UnoconvDoc() (API, error)
// }

// Descriptor returns a Unoconv's module descriptor.
func (UnoconvDoc) Descriptor() gotenberg.ModuleDescriptor {
	return gotenberg.ModuleDescriptor{
		ID:  "unoconvdoc",
		New: func() gotenberg.Module { return new(UnoconvDoc) },
	}
}

// Provision sets the module properties.
// It returns an error if the environment variable UNOCONV_BIN_PATH is not set.
func (mod *UnoconvDoc) Provision(ctx *gotenberg.Context) error {
	// provider, err := ctx.Module(new(Provider))
	// if err != nil {
	// 	return fmt.Errorf("get UnoconvDoc provider: %w", err)
	// }

	// uno, err := provider.(Provider).UnoconvDoc()
	// if err != nil {
	// 	return fmt.Errorf("get UnoconvDoc API: %w", err)
	// }

	// mod.unoconv = uno

	// provider, err = ctx.Module(new(gotenberg.PDFEngineProvider))
	// if err != nil {
	// 	return fmt.Errorf("get PDF engine provider: %w", err)
	// }

	// engine, err := provider.(gotenberg.PDFEngineProvider).PDFEngine()
	// if err != nil {
	// 	return fmt.Errorf("get PDF engine: %w", err)
	// }

	// mod.engine = engine

	binPath, ok := os.LookupEnv("UNOCONV_BIN_PATH")
	if !ok {
		return errors.New("UNOCONV_BIN_PATH environment variable is not set")
	}

	mod.binPath = binPath

	return nil
}

// Validate validates the module properties.
func (mod UnoconvDoc) Validate() error {
	_, err := os.Stat(mod.binPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("unoconv binary path does not exist: %w", err)
	}

	return nil
}

// Metrics returns the metrics.
func (mod UnoconvDoc) Metrics() ([]gotenberg.Metric, error) {
	return []gotenberg.Metric{
		{
			Name:        "unoconvdoc_active_instances_count",
			Description: "Current number of active LibreOffice instances for doc conversion.",
			Read: func() float64 {
				activeInstancesCountMu.RLock()
				defer activeInstancesCountMu.RUnlock()

				return activeInstancesCount
			},
		},
	}, nil
}

// Unoconvdoc returns an API for interacting with unoconv.
func (mod UnoconvDoc) UnoconvDoc() (API, error) {
	return mod, nil
}

// PDF converts a document to PDF. It creates a dedicated LibreOffice instance
// thanks to a custom user profile directory and a free port. Substantial calls
// to this method may increase CPU and memory usage drastically. In such a
// scenario, the given context may also be done before the end of the
// conversion.
func (mod UnoconvDoc) DOC(ctx context.Context, logger *zap.Logger, inputPath, outputPath string, options Options) error {
	port, err := func() (int, error) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, fmt.Errorf("listen on the local network address: %w", err)
		}
		defer func() {
			err := listener.Close()
			if err != nil {
				logger.Error(fmt.Sprintf("close listener: %s", err.Error()))
			}
		}()

		addr := listener.Addr().String()

		_, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return 0, fmt.Errorf("get free port from host: %w", err)
		}

		return strconv.Atoi(portStr)
	}()

	if err != nil {
		return fmt.Errorf("get free port: %w", err)
	}

	userProfileDirPath := gotenberg.NewDirPath()

	args := []string{
		"--user-profile",
		fmt.Sprintf("//%s", userProfileDirPath),
		"--port",
		fmt.Sprintf("%d", port),
		"--format",
		"doc",
	}

	checkedEntry := logger.Check(zap.DebugLevel, "check for debug level before setting high verbosity")
	if checkedEntry != nil {
		args = append(args, "-vvv")
	}

	if options.Landscape {
		args = append(args, "--printer", "PaperOrientation=landscape")
	}

	if options.PageRanges != "" {
		args = append(args, "--export", fmt.Sprintf("PageRange=%s", options.PageRanges))
	}

	args = append(args, "--output", outputPath, inputPath)

	cmd, err := gotenberg.CommandContext(ctx, logger, mod.binPath, args...)
	if err != nil {
		return fmt.Errorf("create unoconv command: %w", err)
	}

	logger.Debug(fmt.Sprintf("print to DOC with: %+v", options))

	activeInstancesCountMu.Lock()
	activeInstancesCount += 1
	activeInstancesCountMu.Unlock()

	err = cmd.Exec()

	activeInstancesCountMu.Lock()
	activeInstancesCount -= 1
	activeInstancesCountMu.Unlock()

	// Always remove the user profile directory created by LibreOffice.
	// See https://github.com/gotenberg/gotenberg/issues/192.
	go func() {
		logger.Debug(fmt.Sprintf("remove user profile directory '%s'", userProfileDirPath))

		err := os.RemoveAll(userProfileDirPath)
		if err != nil {
			logger.Error(fmt.Sprintf("remove user profile directory: %s", err))
		}
	}()

	if err == nil {
		return nil
	}

	// Unoconv/LibreOffice errors are not explicit.
	// That's why we have to make an educated guess according to the exit code
	// and given inputs.

	if strings.Contains(err.Error(), "exit status 5") && options.PageRanges != "" {
		return ErrMalformedPageRanges
	}

	// Possible errors:
	// 1. Unoconv/LibreOffice failed for some reason.
	// 2. Context done.
	//
	// On the second scenario, LibreOffice might not had time to remove some of
	// its temporary files, as it has been killed without warning. The garbage
	// collector will delete them for us (if the module is loaded).
	return fmt.Errorf("unoconvdoc: %w", err)
}

// Extensions returns the file extensions available with unoconv.
func (mod UnoconvDoc) Extensions() []string {
	return []string{
		".bib",
		".doc",
		".xml",
		".docx",
		".fodt",
		".html",
		".ltx",
		".txt",
		".odt",
		".ott",
		".pdb",
		".pdf",
		".psw",
		".rtf",
		".sdw",
		".stw",
		".sxw",
		".uot",
		".vor",
		".wps",
		".epub",
		".png",
		".bmp",
		".emf",
		".eps",
		".fodg",
		".gif",
		".jpg",
		".jpeg",
		".met",
		".odd",
		".otg",
		".pbm",
		".pct",
		".pgm",
		".ppm",
		".ras",
		".std",
		".svg",
		".svm",
		".swf",
		".sxd",
		".sxw",
		".tif",
		".tiff",
		".xhtml",
		".xpm",
		".odp",
		".fodp",
		".potm",
		".pot",
		".pptx",
		".pps",
		".ppt",
		".pwp",
		".sda",
		".sdd",
		".sti",
		".sxi",
		".uop",
		".wmf",
		".csv",
		".dbf",
		".dif",
		".fods",
		".ods",
		".ots",
		".pxl",
		".sdc",
		".slk",
		".stc",
		".sxc",
		".uos",
		".xls",
		".xlt",
		".xlsx",
	}
}

// Routes returns the HTTP routes.
func (mod UnoconvDoc) Routes() ([]api.Route, error) {
	return []api.Route{
		convertRoute(mod),
	}, nil
}

var (
	activeInstancesCount   float64
	activeInstancesCountMu sync.RWMutex
)

// Interface guards.
var (
	_ gotenberg.Module          = (*UnoconvDoc)(nil)
	_ gotenberg.Provisioner     = (*UnoconvDoc)(nil)
	_ gotenberg.Validator       = (*UnoconvDoc)(nil)
	_ gotenberg.MetricsProvider = (*UnoconvDoc)(nil)
	_ API                       = (*UnoconvDoc)(nil)
	_ api.Router                = (*UnoconvDoc)(nil)
	// _ Provider                  = (*UnoconvDoc)(nil)
)
