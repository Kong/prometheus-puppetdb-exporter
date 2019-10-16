package main

import (
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"

	"github.com/camptocamp/prometheus-puppetdb-exporter/internal/exporter"
)

// Config stores handler's configuration
type Config struct {
	Version        bool   `long:"version" description:"Show version."`
	PuppetDBUrl    string `short:"u" long:"puppetdb-url" description:"PuppetDB base URL." env:"PUPPETDB_URL" required:"true"`
	CertFile       string `long:"cert-file" description:"A PEM encoded certificate file." env:"PUPPETDB_CERT_FILE"`
	KeyFile        string `long:"key-file" description:"A PEM encoded private key file." env:"PUPPETDB_KEY_FILE"`
	CACertFile     string `long:"ca-file" description:"A PEM encoded CA's certificate." env:"PUPPETDB_CA_FILE"`
	SSLSkipVerify  bool   `long:"ssl-skip-verify" description:"Skip SSL verification." env:"PUPPETDB_SSL_SKIP_VERIFY"`
	ListenAddress  string `long:"listen-address" description:"Address to listen on for web interface and telemetry." env:"PUPPETDB_LISTEN_ADDRESS" default:"0.0.0.0:9121"`
	MetricPath     string `long:"metric-path" description:"Path under which to expose metrics." env:"PUPPETDB_METRIC_PATH" default:"/metrics"`
	Verbose        bool   `long:"verbose" description:"Enable debug mode" env:"PUPPETDB_VERBOSE"`
	UnreportedNode string `long:"unreported-node" description:"Tag nodes as unreported if the latest report is older than the defined duration." env:"PUPPETDB_UNREPORTED_NODE" default:"2h"`
	Categories     string `long:"categories" description:"Report metrics categories to scrape." env:"REPORT_METRICS_CATEGORIES" default:"resources,time,changes,events"`
}

var (
	// VERSION, BUILD_DATE, GIT_COMMIT are filled in by the build script
	version    = "<<< filled in by build >>>"
	buildDate  = "<<< filled in by build >>>"
	commitSha1 = "<<< filled in by build >>>"
)

func main() {
	var c Config
	parser := flags.NewParser(&c, flags.Default)
	if _, err := parser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		} else {
			os.Exit(1)
		}
	}

	log.Printf("PuppetDB Metrics Exporter %s    build date: %s    sha1: %s    Go: %s",
		version, buildDate, commitSha1,
		runtime.Version(),
	)
	if c.Verbose {
		log.SetLevel(log.DebugLevel)
		log.Debugln("Enabling debug output")
	} else {
		log.SetLevel(log.InfoLevel)
	}

	if c.Version {
		return
	}

	unreportedDuration, err := time.ParseDuration(c.UnreportedNode)
	if err != nil {
		log.Fatalf("failed to parse unreported duration: %s", err)
	}

	// Create a map[string]struct{} of categories to provide an efficient way to
	// find if a category exists in the list of categories.
	cats := strings.Split(c.Categories, ",")
	categories := make(map[string]struct{}, len(cats))
	for _, category := range cats {
		categories[category] = struct{}{}
	}

	registry := prometheus.NewPedanticRegistry()
	_, err = exporter.NewPuppetDBExporter(&exporter.Config{
		URL:                c.PuppetDBUrl,
		CertPath:           c.CertFile,
		CACertPath:         c.CACertFile,
		KeyPath:            c.KeyFile,
		SSLVerify:          !c.SSLSkipVerify,
		Categories:         categories,
		UnreportedDuration: unreportedDuration,
	}, registry)
	if err != nil {
		log.Fatalf("failed to initialize exporter: %s", err)
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "puppetdb_exporter_build_info",
		Help: "puppetdb exporter build informations",
	}, []string{"version", "commit_sha", "build_date", "golang_version"})
	buildInfo.WithLabelValues(version, commitSha1, buildDate, runtime.Version()).Set(1)
	registry.MustRegister(buildInfo)

	http.Handle(c.MetricPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`
<html>
<head><title>Prometheus PuppetDB Exporter v` + version + `</title></head>
<body>
<h1>Prometheus PuppetDB Exporter ` + version + `</h1>
<p><a href='` + c.MetricPath + `'>Metrics</a></p>
</body>
</html>
						`))
	})

	log.Infof("Providing metrics at %s%s", c.ListenAddress, c.MetricPath)
	log.Fatal(http.ListenAndServe(c.ListenAddress, nil))
}
