package finalize

import (
	"fmt"
	"github.com/andibrunner/libbuildpack"
	"golang"
	"io"
	"io/ioutil"
	"kibana/util"
	"os"
	"path/filepath"
)

type Command interface {
	Execute(string, io.Writer, io.Writer, string, ...string) error
}

type Stager interface {
	BuildDir() string
	ClearDepDir() error
	DepDir() string
	DepsIdx() string
	WriteProfileD(string, string) error
}

type Finalizer struct {
	Stager  Stager
	Command Command
	Log     *libbuildpack.Logger
}

func NewFinalizer(stager Stager, command Command, logger *libbuildpack.Logger) (*Finalizer, error) {
	config := struct {
		Config struct {
			KibanaVersion string `yaml:"KibanaVersion"`
		} `yaml:"config"`
	}{}
	if err := libbuildpack.NewYAML().Load(filepath.Join(stager.DepDir(), "config.yml"), &config); err != nil {
		logger.Error("Unable to read config.yml: %s", err.Error())
		return nil, err
	}

	return &Finalizer{
		Stager:  stager,
		Command: command,
		Log:     logger,
	}, nil
}

func Run(gf *Finalizer) error {

	if err := os.MkdirAll(filepath.Join(gf.Stager.BuildDir(), "bin"), 0755); err != nil {
		gf.Log.Error("Unable to create <build-dir>/bin: %s", err.Error())
		return err
	}

	if err := gf.CreateStartupEnvironment("/tmp"); err != nil {
		gf.Log.Error("Unable to create startup scripts: %s", err.Error())
		return err
	}

	return nil
}

func (gf *Finalizer) CreateStartupEnvironment(tempDir string) error {

	//create start script

	content := util.TrimLines(fmt.Sprintf(`
				echo "--> STARTING UP ..."
				MemLimits="$(echo ${VCAP_APPLICATION} | $JQ_HOME/jq '.limits.mem')"

				echo "--> container memory limit = ${MemLimits}m"
				if [ -n "$K_BP_NODE_OPTS" ] || [ -z "$MemLimits" ] || [ -z "$K_BP_RESERVED_MEMORY"  ] || [ -z "$K_BP_HEAP_PERCENTAGE" ] ; then
					export NODE_OPTIONS=$K_BP_NODE_OPTS
					echo "--> Using NODE_OPTIONS=\"${NODE_OPTIONS}\" (user defined)"
				else
					HeapSize=$(( ($MemLimits - $K_BP_RESERVED_MEMORY) / 100 * $K_BP_HEAP_PERCENTAGE ))
					export NODE_OPTIONS="--max-old-space-size=${HeapSize}"
					echo "--> Using NODE_OPTIONS=\"${NODE_OPTIONS}\" (calculated max heap size)"
				fi

				echo "--> preparing runtime directories ..."
				mkdir -p conf.d

				if [ -d kibana.conf.d ] ; then
					rm -rf kibana.conf.d
				fi
				mkdir -p kibana.conf.d

				if [ -d kibana.config ] ; then
					rm -rf kibana.config
				fi
				mkdir -p kibana.config

				echo "--> template processing ..."
				$GTE_HOME/gte $HOME/conf.d $HOME/kibana.conf.d
				$GTE_HOME/gte $K_ROOT/conf.d $HOME/kibana.conf.d

				echo "--> concatenating config files ..."
				awk 'FNR==1{print ""}1' $HOME/kibana.conf.d/* > $HOME/kibana.config/kibana.yml


				echo "--> STARTING KIBANA ..."
				if [ -n "$K_CMD_ARGS" ] ; then
					echo "--> using cmd_args=\"$K_CMD_ARGS\""
				fi

				if [ -n "$K_DO_SLEEP" ] ; then
					sleep 3600
				fi

				chmod +x $HOME/bin/*.sh
				$KIBANA_HOME/bin/kibana -c $HOME/kibana.config/kibana.yml $K_CMD_ARGS
				`))

	err := ioutil.WriteFile(filepath.Join(gf.Stager.BuildDir(), "bin/run.sh"), []byte(content), 0755)
	if err != nil {
		gf.Log.Error("Unable to write start script: %s", err.Error())
		return err
	}

	//create release yml
	err = ioutil.WriteFile(filepath.Join(tempDir, "buildpack-release-step.yml"), []byte(golang.ReleaseYAML("bin/run.sh")), 0644)
	if err != nil {
		gf.Log.Error("Unable to write release yml: %s", err.Error())
		return err
	}

	return gf.Stager.WriteProfileD("go.sh", golang.GoScript())
}
