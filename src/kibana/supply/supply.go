package supply

import (
	"github.com/andibrunner/libbuildpack"
	"os"
	"path/filepath"
	"strings"

	"fmt"
	"io/ioutil"
	conf "kibana/config"

	"errors"
	"kibana/util"
	"os/exec"
	"encoding/json"
)

type Manifest interface {
	AllDependencyVersions(string) []string
	DefaultVersion(string) (libbuildpack.Dependency, error)
	InstallDependency(libbuildpack.Dependency, string) error
	InstallDependencyWithCache(libbuildpack.Dependency, string, string) error
	InstallOnlyVersion(string, string) error
	IsCached() bool
}

type Stager interface {
	AddBinDependencyLink(string, string) error
	BuildDir() string
	CacheDir() string
	DepDir() string
	DepsIdx() string
	WriteConfigYml(interface{}) error
	WriteEnvFile(string, string) error
	WriteProfileD(string, string) error
}

type Supplier struct {
	Stager               Stager
	Manifest             Manifest
	Log                  *libbuildpack.Logger
	BuildpackDir         string
	CachedDeps           map[string]string
	DepCacheDir			 string
	GTE                  Dependency
	Jq                   Dependency
	Kibana               Dependency
	KibanaPlugins        Dependency
	XPack                Dependency
	KibanaConfig         conf.KibanaConfig
	TemplatesConfig      conf.TemplatesConfig
	VcapApp              conf.VcapApp
	VcapServices         conf.VcapServices
	ConfigFilesExists    bool
	TemplatesToInstall   []conf.Template
	PluginsToInstall     map[string]string
}

type Dependency struct {
	Name            string
	DirName         string
	Version         string
	VersionParts    int
	ConfigVersion   string
	RuntimeLocation string
	StagingLocation string
}

func Run(gs *Supplier) error {

	//Init maps for the Installation
	gs.DepCacheDir = filepath.Join(gs.Stager.CacheDir(), "dependencies")
	gs.PluginsToInstall = make(map[string]string)
	gs.TemplatesToInstall = []conf.Template{}

	//Eval Kibana file
	if err := gs.EvalKibanaFile(); err != nil {
		gs.Log.Error("Unable to evaluate Kibana file: %s", err.Error())
		return err
	}

	//Set log level
	if strings.ToLower(gs.KibanaConfig.Buildpack.LogLevel) == "debug" {
		os.Setenv("BP_DEBUG", "true")
	}

	//Init Cache
	if err := gs.ReadCachedDependencies(); err != nil {
		return err
	}

	//Show Depug Infos
	if err := gs.EvalTestCache(); err != nil {
		gs.Log.Error("Unable to test cache: %s", err.Error())
		return err
	}

	//Prepare dir structure
	if err := gs.PrepareAppDirStructure(); err != nil {
		gs.Log.Error("Unable to prepare directory structure for the app: %s", err.Error())
		return err
	}

	//Eval Templates file
	if err := gs.EvalTemplatesFile(); err != nil {
		gs.Log.Error("Unable to evaluate Templates file: %s", err.Error())
		return err
	}

	//Eval Environment
	if err := gs.EvalEnvironment(); err != nil {
		gs.Log.Error("Unable to evaluate environment: %s", err.Error())
		return err
	}

	//Install Dependencies
	if err := gs.InstallDependencyGTE(); err != nil {
		return err
	}
	if err := gs.InstallDependencyJq(); err != nil {
		return err
	}

	//Prepare Staging Environment
	if err := gs.PrepareStagingEnvironment(); err != nil {
		return err
	}

	//Install templates
	if err := gs.InstallTemplates(); err != nil {
		gs.Log.Error("Unable to install template file: %s", err.Error())
		return err
	}

	//Install User Certificates
	if err := gs.InstallUserCertificates(); err != nil {
		return err
	}

	//Install Kibana
	if err := gs.InstallKibana(); err != nil {
		return err
	}

	//Install Kibana Plugins
	if len(gs.PluginsToInstall) > 0 { // there are plugins to install

		//Install Kibana Plugins Dependencies from S3
		for key, _ := range gs.PluginsToInstall {
			if strings.HasPrefix(key, "x-pack") { //is x-pack plugin
				if err := gs.InstallDependencyXPack(); err != nil {
					return err
				}
				break
			}
		}

		for key, _ := range gs.PluginsToInstall {
			if !strings.HasPrefix(key, "x-pack") { //other than  x-pack plugin
				if err := gs.InstallDependencyKibanaPlugins(); err != nil {
					return err
				}
				break
			}
		}

		//Install Kibana Plugins
		if err := gs.InstallKibanaPlugins(); err != nil {
			return err
		}
	}

	//List Kibana Plugins
	if err := gs.ListKibanaPlugins(); err != nil {
		return err
	}

	// Remove orphand dependencies from application cache
	gs.RemoveUnusedDependencies()

	//WriteConfigYml
	config := map[string]string{
		"KibanaVersion": gs.Kibana.Version,
	}

	if err := gs.Stager.WriteConfigYml(config); err != nil {
		gs.Log.Error("Error writing config.yml: %s", err.Error())
		return err
	}

	return nil
}

func (gs *Supplier) EvalTestCache() error {

	if strings.ToLower(gs.KibanaConfig.Buildpack.LogLevel) == "debug" {
		gs.Log.Debug("----> Show staging directories:")
		gs.Log.Debug("        Cache dir: %s", gs.Stager.CacheDir())
		gs.Log.Debug("        Build dir: %s", gs.Stager.BuildDir())
		gs.Log.Debug("        Buildpack dir: %s", gs.BPDir())
		gs.Log.Debug("        Dependency dir: %s", gs.Stager.DepDir())
		gs.Log.Debug("        DepsIdx: %s", gs.Stager.DepsIdx())

	}
	return nil
}

func (gs *Supplier) EvalKibanaFile() error {
	const configCheck = false
	const reservedMemory  = 300
	const heapPersentage = 90
	const logLevel = "Info"
	const noCache = false
	const curatorInstall = false

	gs.KibanaConfig = conf.KibanaConfig{
		Set:            true,
		ConfigCheck:    configCheck,
		ReservedMemory: reservedMemory,
		HeapPercentage: heapPersentage,
		Buildpack:      conf.Buildpack{Set: true, LogLevel: logLevel, NoCache: noCache}}

	KibanaFile := filepath.Join(gs.Stager.BuildDir(), "Kibana")

	data, err := ioutil.ReadFile(KibanaFile)
	if err != nil {
		return err
	}
	if err := gs.KibanaConfig.Parse(data); err != nil {
		return err
	}

	if !gs.KibanaConfig.Set {
		gs.KibanaConfig.HeapPercentage = heapPersentage
		gs.KibanaConfig.ReservedMemory = reservedMemory
		gs.KibanaConfig.ConfigCheck = configCheck
	}
	if !gs.KibanaConfig.Buildpack.Set {
		gs.KibanaConfig.Buildpack.LogLevel = logLevel
		gs.KibanaConfig.Buildpack.NoCache = noCache
	}

	/*	//Eval X-Pack
		if gs.KibanaConfig.XPack.Monitoring.Enabled || gs.KibanaConfig.XPack.Management.Enabled{
			gs.KibanaConfig.Plugins = append(gs.KibanaConfig.Plugins, "x-pack")

			if gs.KibanaConfig.XPack.Management.Interval == ""{
				gs.KibanaConfig.XPack.Management.Interval = "10s"
			}
			if gs.KibanaConfig.XPack.Monitoring.Interval == ""{
				gs.KibanaConfig.XPack.Monitoring.Interval = "10s"
			}

		}
	*/
	//ToDo Eval values

	//copy the user defined plugins to the PluginsToInstall map
	for i := 0; i < len(gs.KibanaConfig.Plugins); i++ {
		gs.PluginsToInstall[gs.KibanaConfig.Plugins[i]] = ""
	}

	return nil
}

func (gs *Supplier) PrepareAppDirStructure() error {

	//create dir conf.d in DepDir
	dir := filepath.Join(gs.Stager.DepDir(), "conf.d")
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir plugins in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "plugins")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir certificates in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "certificates")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) EvalTemplatesFile() error {

	const credHostField = "host"
	const credUsernameField = "username"
	const credPasswordField = "password"

	gs.TemplatesConfig = conf.TemplatesConfig{
		Set:            true,
		Alias:        conf.Alias{Set: true, CredentialsHostField: credHostField, CredentialsUsernameField: credUsernameField, CredentialsPasswordField: credPasswordField},
    }
	templateFile := filepath.Join(gs.BPDir(), "defaults/templates/templates.yml")

	data, err := ioutil.ReadFile(templateFile)
	if err != nil {
		return err
	}
	if err := gs.TemplatesConfig.Parse(data); err != nil {
		return err
	}
	if !gs.TemplatesConfig.Alias.Set {
		gs.TemplatesConfig.Alias.CredentialsHostField = credHostField
		gs.TemplatesConfig.Alias.CredentialsUsernameField = credUsernameField
		gs.TemplatesConfig.Alias.CredentialsPasswordField = credPasswordField
	}

	return nil
}

func (gs *Supplier) EvalEnvironment() error {

	//get VCAP_APPLICATIOM
	gs.VcapApp = conf.VcapApp{}
	dataApp := os.Getenv("VCAP_APPLICATION")
	if err := gs.VcapApp.Parse([]byte(dataApp)); err != nil {
		return err
	}

	// get VCAP_SERVICES
	gs.VcapServices = conf.VcapServices{}
	dataServices := os.Getenv("VCAP_SERVICES")
	if err := gs.VcapServices.Parse([]byte(dataServices)); err != nil {
		return err
	}

	//check if files (also directories) exist in the application's "conf.d" directory
	configDir := filepath.Join(gs.Stager.BuildDir(), "conf.d")
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		gs.ConfigFilesExists = false
		return nil
	}

	files, err := ioutil.ReadDir(configDir)
	if err != nil {
		return err
	}
	if len(files) > 0 {
		gs.ConfigFilesExists = true
	}
	
	return nil
}

func (gs *Supplier) InstallDependencyGTE() error {
	var err error

	gs.GTE, err = gs.NewDependency("gte", 3, "")
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.GTE); err != nil {
		return err
	}

	content := util.TrimLines(fmt.Sprintf(`
				export GTE_HOME=$DEPS_DIR/%s
				PATH=$PATH:$GTE_HOME
				`, gs.GTE.RuntimeLocation))

	if err := gs.WriteDependencyProfileD(gs.GTE.Name, content); err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) InstallDependencyJq() error {
	var err error

	gs.Jq, err = gs.NewDependency("jq", 3, "")
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.Jq); err != nil {
		return err
	}

	content := util.TrimLines(fmt.Sprintf(`
				export JQ_HOME=$DEPS_DIR/%s
				PATH=$PATH:$JQ_HOME
				`, gs.Jq.RuntimeLocation))

	if err := gs.WriteDependencyProfileD(gs.Jq.Name, content); err != nil {
		return err
	}
	return nil
}


func (gs *Supplier) InstallDependencyXPack() error {

	//Install x-pack from S3
	var err error
	gs.XPack, err = gs.NewDependency("x-pack", 3, gs.KibanaConfig.Version) //same version as Kibana
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.XPack); err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) InstallDependencyKibanaPlugins() error {

	//Install Kibana-plugins from S3
	var err error
	gs.KibanaPlugins, err = gs.NewDependency("kibana-plugins", 3, gs.KibanaConfig.Version) //same version as Kibana
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.KibanaPlugins); err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) InstallKibana() error {
	var err error
	gs.Kibana, err = gs.NewDependency("kibana", 3, gs.KibanaConfig.Version)
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.Kibana); err != nil {
		return err
	}

	sleepCommand := ""
	if gs.KibanaConfig.Buildpack.DoSleepCommand {
		sleepCommand = "yes"
	}
	content := util.TrimLines(fmt.Sprintf(`
			export K_BP_RESERVED_MEMORY=%d
			export K_BP_HEAP_PERCENTAGE=%d
			export K_BP_NODE_OPTS=%s
			export K_CMD_ARGS=%s
			export K_ROOT=$DEPS_DIR/%s
			export KIBANA_HOME=$DEPS_DIR/%s
			export K_DO_SLEEP=%s
			PATH=$PATH:$KIBANA_HOME/bin
			`,
		gs.KibanaConfig.ReservedMemory,
		gs.KibanaConfig.HeapPercentage,
		gs.KibanaConfig.NodeOpts,
		gs.KibanaConfig.CmdArgs,
		gs.Stager.DepsIdx(),
		gs.Kibana.RuntimeLocation,
		sleepCommand))

	if err := gs.WriteDependencyProfileD(gs.Kibana.Name, content); err != nil {
		gs.Log.Error("Error writing profile.d script for Kibana: %s", err.Error())
		return err
	}


	return nil
}

func (gs *Supplier) PrepareStagingEnvironment() error {
	os.Setenv("PORT", "8080") //dummy PORT: used by template processing for Kibana check
	return nil
}

func (gs *Supplier) InstallUserCertificates() error {

	var certArray = []string{}

	if len(gs.KibanaConfig.Certificates) == 0 { // no certificates to install
		return nil
	}

	localCerts, _ := gs.ReadLocalCertificates(gs.Stager.BuildDir() + "/certificates")

	for i := 0; i < len(gs.KibanaConfig.Certificates); i++ {

		localCert := localCerts[gs.KibanaConfig.Certificates[i]]
		if localCert != "" {
			gs.Log.Info(fmt.Sprintf("----> adding user certificate '%s' ... ", gs.KibanaConfig.Certificates[i]))
			certArray = append(certArray, fmt.Sprintf("$HOME/certificates/%s", localCert))
		} else {
			err := errors.New("crt file for certificate not found in directory")
			gs.Log.Error("File %s.crt not found in directory '/certificates'", gs.KibanaConfig.Certificates[i])
			return err
		}
	}

	if len(certArray) > 0 {
		jsonCertArray, err := json.Marshal(certArray)
		if err != nil {
			return err
		}
		content := util.TrimLines(fmt.Sprintf(`
				export K_CERTS="%s"
				`, strings.Replace( string(jsonCertArray) ,`"`,`\"`,-1) ))

		if err := gs.WriteDependencyProfileD("certificates", content); err != nil {
			return err
		}

	}

	return nil

}

func (gs *Supplier) InstallTemplates() error {

	if !gs.ConfigFilesExists && len(gs.KibanaConfig.ConfigTemplates) == 0 {
		// install all default templates

		//copy default templates to config
		for _, t := range gs.TemplatesConfig.Templates {

			if t.IsDefault {

				if len(t.Tags) > 0 {
					vcapServices := []conf.VcapService{}
					vcapServicesWithTag := gs.VcapServices.WithTags(t.Tags)
					vcapServicesUserProvided := gs.VcapServices.UserProvided()

					if len(vcapServicesWithTag) > 0 {
						vcapServices = append(vcapServices, vcapServicesWithTag...)
					}
					if len(vcapServicesUserProvided) > 0 {
						vcapServices = append(vcapServices, vcapServicesUserProvided...)
					}

					if len(vcapServices) == 0 {

						if gs.KibanaConfig.EnableServiceFallback {
							ti := t
							ti.ServiceInstanceName = ""
							gs.TemplatesToInstall = append(gs.TemplatesToInstall, ti)
							gs.Log.Warning("No service found for template %s, will do the fallback. Please bind a service and restage the app", ti.Name)
						} else {
							return errors.New("no service found for template")
						}
					} else if len(vcapServices) > 1 {
						return errors.New("more than one service found for template")
					} else {
						ti := t
						ti.ServiceInstanceName = vcapServices[0].Name
						gs.TemplatesToInstall = append(gs.TemplatesToInstall, ti)
					}
				} else {
					ti := t
					ti.ServiceInstanceName = ""
					gs.TemplatesToInstall = append(gs.TemplatesToInstall, ti)
				}
			}
		}

	} else {
		//only install explicitly defined templates, if any
		//check them all

		for _, ct := range gs.KibanaConfig.ConfigTemplates {
			found := false
			templateName := strings.Trim(ct.Name, " ")
			if len(templateName) == 0 {
				gs.Log.Warning("Skipping template: no valid name defined for template in Kibana file")
				continue
			}
			for _, t := range gs.TemplatesConfig.Templates {
				if templateName == t.Name {
					serviceInstanceName := strings.Trim(ct.ServiceInstanceName, " ")
					if len(serviceInstanceName) == 0 && len(t.Tags) > 0 {
						gs.Log.Error("Template %s requires service instance name: No service instance name defined for template in Kibana file", templateName)
						return errors.New("no service instance name defined for template in Kibana file")
					}

					ti := t
					if len(serviceInstanceName) > 0 && len(t.Tags) == 0 {
						gs.Log.Warning("Service instance name '%s' is defined for template %s in Kibana file but template can not be bound to a service.", serviceInstanceName, templateName)
					} else {
						ti.ServiceInstanceName = serviceInstanceName
					}
					gs.TemplatesToInstall = append(gs.TemplatesToInstall, ti)

					found = true
					break
				}
			}
			if !found {
				gs.Log.Warning("Template %s defined in Kibana file does not exist", templateName)
			}
		}
	}

	//copy templates --> conf.d
	for _, ti := range gs.TemplatesToInstall {

		os.Setenv("SERVICE_INSTANCE_NAME", ti.ServiceInstanceName)
		os.Setenv("CREDENTIALS_HOST_FIELD", gs.TemplatesConfig.Alias.CredentialsHostField)
		os.Setenv("CREDENTIALS_USERNAME_FIELD", gs.TemplatesConfig.Alias.CredentialsUsernameField)
		os.Setenv("CREDENTIALS_PASSWORD_FIELD", gs.TemplatesConfig.Alias.CredentialsPasswordField)

		templateFile := filepath.Join(gs.BPDir(), "defaults/templates/", ti.Name+".yml")
		destFile := filepath.Join(gs.Stager.DepDir(), "conf.d", ti.Name+".yml")

		err := exec.Command(fmt.Sprintf("%s/gte", gs.GTE.StagingLocation), "-d", "<<:>>", templateFile, destFile).Run()
		if err != nil {
			gs.Log.Error("Error pre-processing template %s: %s", ti.Name, err.Error())
			return err
		}

	}

	// copy grok-patterns, mappings and plugins
	for i := 0; i < len(gs.TemplatesToInstall); i++ {
		for p := 0; p < len(gs.TemplatesToInstall[i].Plugins); p++ {
			gs.PluginsToInstall[gs.TemplatesToInstall[i].Plugins[p]] = ""
		}
	}

	//default Plugins will be installed in method "InstallKibanaPlugins"

	return nil
}

func (gs *Supplier) ListKibanaPlugins() error {
	gs.Log.Info("----> Listing all installed Kibana plugins ...")

	out, err := exec.Command(fmt.Sprintf("%s/bin/kibana-plugin", gs.Kibana.StagingLocation), "list").CombinedOutput()
	gs.Log.Info(string(out))
	if err != nil {
		gs.Log.Error("Error listing all installed Kibana plugins: %s", err.Error())
		return err
	}
	return nil
}

func (gs *Supplier) InstallKibanaPlugins() error {

	xPackPlugins, _ := gs.ReadLocalPlugins(gs.XPack.StagingLocation)
	defaultPlugins, _ := gs.ReadLocalPlugins(gs.KibanaPlugins.StagingLocation)
	userPlugins, _ := gs.ReadLocalPlugins(gs.Stager.BuildDir() + "/plugins/")

	gs.Log.Info("----> Installing Kibana plugins (this can take a few minutes!) ...")
	for key, _ := range gs.PluginsToInstall {
		//Priorisation
		xpackPlugin := gs.GetLocalPlugin(key, xPackPlugins)
		defaultPlugin := gs.GetLocalPlugin(key, defaultPlugins)
		userPlugin := gs.GetLocalPlugin(key, userPlugins)

		pluginToInstall := ""

		if xpackPlugin != "" {
			pluginToInstall = filepath.Join(gs.XPack.StagingLocation, xpackPlugin) // Prio 1 (offline installation)
		} else if defaultPlugin != "" {
			pluginToInstall = filepath.Join(gs.KibanaPlugins.StagingLocation, defaultPlugin) // Prio 2 (offline installation)
		} else if userPlugin != "" {
			pluginToInstall = filepath.Join(gs.Stager.BuildDir(), "plugins", userPlugin) // Prio 3 (offline installation)
		} else {
			pluginToInstall = key // Prio 4 (online installation)
		}

		if strings.HasSuffix(pluginToInstall, ".zip") && !(strings.HasPrefix(pluginToInstall, "http://") || strings.HasPrefix(pluginToInstall, "https://")){
			pluginToInstall = "file://" + pluginToInstall
		}

		//Install Plugin
		gs.Log.Info("       - installing plugin %s", key)
		out, err := exec.Command(fmt.Sprintf("%s/bin/kibana-plugin", gs.Kibana.StagingLocation), "install", pluginToInstall).CombinedOutput()
		if err != nil {
			gs.Log.Error(string(out))
			gs.Log.Error("Error installing Kibana plugin %s: %s", key, err.Error())
			return err
		}
	}

	return nil
}

func (gs *Supplier) ReadLocalCertificates(filePath string) (map[string]string, error) {

	var localCerts map[string]string
	localCerts = make(map[string]string)

	file, err := os.Open(filePath)
	if err != nil {
		gs.Log.Error("failed opening certificates directory: %s", err)
		return localCerts, err
	}
	defer file.Close()

	list, _ := file.Readdirnames(0) // 0 to read all files and folders
	for _, name := range list {

		if strings.HasSuffix(name, ".crt") {
			certParts := strings.Split(name, ".crt")

			if len(certParts) == 2 {
				certName := certParts[0]
				localCerts[certName] = name
			}

		}
	}

	return localCerts, nil
}

func (gs *Supplier) ReadLocalPlugins(filePath string) ([]string, error) {

	file, err := os.Open(filePath)
	if err != nil {
		return []string{}, nil
	}
	defer file.Close()

	list, _ := file.Readdirnames(0) // 0 to read all files and folders

	return list, nil
}

func (gs *Supplier) GetLocalPlugin(pluginName string, pluginFileNames []string) string {

	for i := 0; i < len(pluginFileNames); i++ {
		if strings.HasPrefix(pluginFileNames[i], pluginName) {
			return pluginFileNames[i]
		}
	}

	return ""
}
