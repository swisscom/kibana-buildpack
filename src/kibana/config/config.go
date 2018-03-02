package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v2"
	"strings"
)

// [BP]/defaults/templates/templates.yml
type TemplatesConfig struct {
	Set         bool             `yaml:"-"`
	Alias 		Alias            `yaml:"alias"`
	Templates []Template         `yaml:"templates"`
}

type Alias struct {
	Set                      bool   `yaml:"-"`
	CredentialsHostField     string `yaml:"credentials-host-field"`
	CredentialsUsernameField string `yaml:"credentials-username-field"`
	CredentialsPasswordField string `yaml:"credentials-password-field"`
}
type Template struct {
	Name                string   `yaml:"name"`
	Type                string   `yaml:"type"`
	IsDefault           bool     `yaml:"is-default"`
	IsFallback          bool     `yaml:"is-fallback"`
	Tags                []string `yaml:"tags"`
	Plugins             []string `yaml:"plugins"`
	ServiceInstanceName string   `yaml:"-"`
}

func (c *TemplatesConfig) Parse(data []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprintf("Yaml parsing error: %s", r))
		}
	}()

	return yaml.Unmarshal(data, c)
}

// [APP]Kibana
type KibanaConfig struct {
	Set                   bool             `yaml:"-"`
	Version               string           `yaml:"version"`
	Plugins               []string         `yaml:"plugins"`
	Certificates          []string         `yaml:"certificates"`
	CmdArgs               string           `yaml:"cmd-args"`
	NodeOpts              string           `yaml:"nodejs-options"`
	ReservedMemory        int              `yaml:"reserved-memory"`
	HeapPercentage        int              `yaml:"heap-percentage"`
	ConfigCheck           bool             `yaml:"config-check"`
	ConfigTemplates       []ConfigTemplate `yaml:"config-templates"`
	EnableServiceFallback bool             `yaml:"enable-service-fallback"`
//	XPack                 XPack            `yaml:"x-pack"`
	Buildpack             Buildpack        `yaml:"buildpack"`
}

type Buildpack struct {
    Set                   bool 			   `yaml:"-"`
	LogLevel              string           `yaml:"log-level"`
	NoCache               bool             `yaml:"no-cache"`
	DoSleepCommand        bool             `yaml:"sleep-command"`
}

type ConfigTemplate struct {
	Name                string `yaml:"name"`
	ServiceInstanceName string `yaml:"service-instance-name"`
}

/*
type XPack struct {
	Set        bool           `yaml:"-"`
	Monitoring XPackComponent `yaml:"monitoring"`
	Management XPackComponent `yaml:"management"`
}

type XPackComponent struct {
	Set      bool   `yaml:"-"`
	Enabled  bool   `yaml:"enabled"`
	Interval string `yaml:"interval"`
}
*/


func (c *KibanaConfig) Parse(data []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprintf("Yaml parsing error: %s", r))
		}
	}()

	return yaml.Unmarshal(data, c)
}

// VCAP_APPLICATION
// An App holds information about the current app running on Cloud Foundry
type VcapApp struct {
	AppID           string   `json:"application_id"`      // id of the application
	Name            string   `json:"application_name"`    // name of the app
	ApplicationURIs []string `json:"application_uris"`    // application uri of the app
	Version         string   `json:"application_version"` // version of the app
	CFAPI           string   `json:"cf_api"`              // URL for the Cloud Foundry API endpoint
	Limits          *Limits  `json:"limits"`              // limits imposed on this process
}

type Limits struct {
	Disk int `json:"disk"` // disk limit
	FDs  int `json:"fds"`  // file descriptors limit
	Mem  int `json:"mem"`  // memory limit
}

func (c *VcapApp) Parse(data []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprintf("Json parsing error: %s", r))
		}
	}()

	return json.Unmarshal(data, c)
}

type VcapServices map[string][]VcapService

type VcapService struct {
	Name        string                 `json:"name"`        // name of the service
	Label       string                 `json:"label"`       // label of the service
	Tags        []string               `json:"tags"`        // tags for the service
	Plan        string                 `json:"plan"`        // plan of the service
	Credentials map[string]interface{} `json:"credentials"` // credentials for the service
}

func (s *VcapServices) WithTags(tags []string) []VcapService {
	result := []VcapService{}
	for _, service_instances := range *s {
		for i := range service_instances {
			service_instance := service_instances[i]
			for _, st := range service_instance.Tags {
				found := false
				for _, t := range tags {
					if strings.EqualFold(t, st) {
						found = true
						result = append(result, service_instance)
						break
					}
				}
				if found {
					break
				}
			}
		}
	}

	return result
}

func (s *VcapServices) UserProvided() []VcapService {
	result := []VcapService{}
	for service , service_instances := range *s {
		if service == "user-provided" {
			for i := range service_instances {
				service_instance := service_instances[i]
				result = append(result, service_instance)
			}
		}
	}

	return result
}


func (c *VcapServices) Parse(data []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprintf("Json parsing error: %s", r))
		}
	}()

	return json.Unmarshal(data, c)
}
