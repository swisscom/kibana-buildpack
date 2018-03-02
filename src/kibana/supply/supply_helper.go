package supply

import (
	"os"
	"strings"
	"github.com/andibrunner/libbuildpack"
	"path/filepath"
	"io/ioutil"
	"fmt"
	"kibana/util"
)


func (gs *Supplier) BPDir() string {
	return gs.BuildpackDir
}

func (gs *Supplier) NewDependency(name string, versionParts int, configVersion string) (Dependency, error){
	var dependency = Dependency{Name: name, VersionParts: versionParts, ConfigVersion: configVersion}

	if parsedVersion, err := gs.SelectDependencyVersion(dependency); err != nil {
		gs.Log.Error("Unable to determine the version of %s: %s", dependency, err.Error())
		return dependency, err
	} else {
		dependency.Version = parsedVersion
		dependency.DirName = dependency.Name+"-"+dependency.Version
		dependency.RuntimeLocation = gs.EvalRuntimeLocation(dependency)
		dependency.StagingLocation = gs.EvalStagingLocation(dependency)
	}

	return dependency, nil
}


func (gs *Supplier) WriteDependencyProfileD(dependencyName string, content string) error {

	if err := gs.Stager.WriteProfileD(dependencyName+".sh", content); err != nil {
		gs.Log.Error("Error writing profile.d script for %s: %s", dependencyName,err.Error())
		return err
	}
	return nil
}

func (gs *Supplier) ReadCachedDependencies() error {

	if gs.KibanaConfig.Buildpack.NoCache {
		gs.Log.Debug("--> cleaning cache")
		err := util.RemoveAllContents(gs.Stager.CacheDir())
		if err != nil{
			gs.Log.Debug("--> error cleaning cache: %s", err.Error())
		}
	}

	gs.CachedDeps = make(map[string]string)
	os.MkdirAll(gs.DepCacheDir,0755)

	cacheDir, err := ioutil.ReadDir(gs.DepCacheDir)
	if err != nil {
		gs.Log.Error("  --> failed reading cache directory: %s", err)
		return err
	}

	for _, dirEntry := range cacheDir{
		gs.Log.Debug(fmt.Sprintf("--> added dependency '%s' to cache list", dirEntry.Name()))
		gs.CachedDeps[dirEntry.Name()] = ""
	}

	return nil
}


func (gs *Supplier) InstallDependency(dependency Dependency) error {
	var err error

	dep := libbuildpack.Dependency{Name: dependency.Name, Version: dependency.Version}

	//check if there are other cached versions of the same dependency
	for cachedDep := range gs.CachedDeps{
		if cachedDep != dependency.DirName && strings.HasPrefix(cachedDep, dependency.Name + "-") {
			gs.Log.Debug(fmt.Sprintf("--> deleting unused dependency version '%s' from application cache", cachedDep))
			gs.CachedDeps[cachedDep] = "deleted"
			os.RemoveAll(filepath.Join(gs.DepCacheDir, cachedDep))
		}
	}

	if err = gs.Manifest.InstallDependencyWithCache(dep, filepath.Join(gs.DepCacheDir,dependency.DirName), dependency.StagingLocation); err != nil {
		gs.Log.Error("Error installing '%s': %s", dependency.Name, err.Error())
		return err
	}

	if gs.KibanaConfig.Buildpack.NoCache {
		os.RemoveAll(filepath.Join(gs.DepCacheDir,dependency.DirName))
	}

	gs.CachedDeps[dependency.DirName] = "in use"

	return nil
}

func (gs *Supplier) RemoveUnusedDependencies () error{

	for cachedDep, value := range gs.CachedDeps{
		if value == "" {
			gs.Log.Debug(fmt.Sprintf("--> deleting unused dependency '%s' from application cache", cachedDep))
			os.RemoveAll(filepath.Join(gs.DepCacheDir, cachedDep))
		}
	}
	return nil
}


func (gs *Supplier) SelectDependencyVersion(dependency Dependency) (string, error) {

	dependencyVersion := dependency.ConfigVersion

	if dependencyVersion == "" {
		defaultDependencyVersion, err := gs.Manifest.DefaultVersion(dependency.Name)
		if err != nil {
			return "", err
		}
		dependencyVersion = defaultDependencyVersion.Version
	}

	return gs.parseDependencyVersion(dependency, dependencyVersion)
}

func (gs *Supplier) parseDependencyVersion(dependency Dependency, partialDependencyVersion string) (string, error) {
	existingVersions := gs.Manifest.AllDependencyVersions(dependency.Name)

	if len(strings.Split(partialDependencyVersion, ".")) < dependency.VersionParts {
		partialDependencyVersion += ".x"
	}

	expandedVer, err := libbuildpack.FindMatchingVersion(partialDependencyVersion, existingVersions)
	if err != nil {
		return "", err
	}

	return expandedVer, nil
}

func (gs *Supplier) EvalRuntimeLocation(dependency Dependency) string {
	return filepath.Join(gs.Stager.DepsIdx(), dependency.DirName)
}

func (gs *Supplier) EvalStagingLocation(dependency Dependency) string {
	return filepath.Join(gs.Stager.DepDir(), dependency.DirName)
}

