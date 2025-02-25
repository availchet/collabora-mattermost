package main

import (
	"encoding/xml"
	"io"
	"reflect"
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

var (
	// WopiFiles maps file extension with file action & url.
	WopiFiles map[string]WopiFile

	// validEncryptionKeyChars ensures that the encryption key only contains letters and numbers.
	validEncryptionKeyChars = regexp.MustCompile("[^a-zA-Z0-9]+")

	// TemplateFromExt stores the name of the template file corresponding to each file extension.
	TemplateFromExt = map[string]string{
		"docx": "docxtemplate.docx",
		"odt":  "odttemplate.odt",
		"pptx": "pptxtemplate.pptx",
		"odp":  "template.odp",
		"xlsx": "xlsxtemplate.xlsx",
		"ods":  "template.ods",
	}

	// WebsocketEventConfigUpdated is the websocket event called to update plugin config on clients' webapp.
	WebsocketEventConfigUpdated = "config_updated"

	// PermissionOwner allows only the owner to edit the file.
	PermissionOwner = "owner"

	// PermissionChannel allows only all channel members to edit the file.
	PermissionChannel = "channel"

	// AllowedFilePermissions is the list of file permissions.
	AllowedFilePermissions = map[string]bool{
		PermissionOwner:   true,
		PermissionChannel: true,
	}
)

// Configuration captures the plugin's external Configuration as exposed in the Mattermost server
// Configuration, as well as values computed from the Configuration. Any public fields will be
// deserialized from the Mattermost server Configuration in OnConfigurationChange.
//
// As plugins are inherently concurrent (hooks being called asynchronously), and the plugin
// Configuration can change at any time, access to the Configuration must be synchronized. The
// strategy used in this plugin is to guard a pointer to the Configuration, and clone the entire
// struct whenever it changes. You may replace this with whatever strategy you choose.
//
// If you add non-reference types to your Configuration struct, be sure to rewrite Clone as a deep
// copy appropriate for your types.
type Configuration struct {
	WOPIAddress         string
	SkipSSLVerify       bool
	EncryptionKey       string
	FileEditPermissions bool
}

// ToWebappConfig initializes the webapp config from the Configuration.
func (c *Configuration) ToWebappConfig() *WebappConfig {
	return &WebappConfig{
		c.FileEditPermissions,
	}
}

// Clone deep copies the Configuration.
func (c *Configuration) Clone() *Configuration {
	return &Configuration{WOPIAddress: c.WOPIAddress}
}

// ProcessConfiguration processes the Configuration.
func (c *Configuration) ProcessConfiguration() {
	// trim trailing slash or spaces from the WOPI address, if needed
	c.WOPIAddress = strings.TrimSpace(c.WOPIAddress)
	c.WOPIAddress = strings.Trim(c.WOPIAddress, "/")
	c.EncryptionKey = validEncryptionKeyChars.ReplaceAllString(c.EncryptionKey, "")
}

// IsValid checks if all needed fields for Configuration are set.
func (c *Configuration) IsValid() error {
	if !strings.HasPrefix(c.WOPIAddress, "http") {
		return errors.New("please provide the WOPIAddress")
	}

	if len(c.EncryptionKey) == 0 {
		return errors.New("please generate EncryptionKey from plugin system console settings")
	}

	return nil
}

// getConfiguration retrieves the active configuration under lock, making it safe to use
// concurrently. The active configuration may change underneath the client of this method, but
// the struct returned by this API call is considered immutable.
func (p *Plugin) getConfiguration() *Configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &Configuration{}
	}

	return p.configuration
}

// setConfiguration replaces the active configuration under lock.
//
// Do not call setConfiguration while holding the configurationLock, as sync.Mutex is not
// reentrant. In particular, avoid using the plugin API entirely, as this may in turn trigger a
// hook back into the plugin. If that hook attempts to acquire this lock, a deadlock may occur.
//
// This method panics if setConfiguration is called with the existing configuration. This almost
// certainly means that the configuration was modified without being cloned and may result in
// an unsafe access.
func (p *Plugin) setConfiguration(configuration *Configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		// Ignore assignment if the configuration struct is empty. Go will optimize the
		// allocation for same to point at the same memory address, breaking the check
		// above.
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}

		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

// LoadWopiFileInfo loads the WOPI file data to memory.
func (p *Plugin) LoadWopiFileInfo(wopiAddress string) error {
	client := p.GetHTTPClient()
	resp, err := client.Get(wopiAddress + "/hosting/discovery")
	if err != nil {
		p.client.Log.Error("WOPI request error. Please check the WOPI address.", err.Error())
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.client.Log.Error("WOPI request error. Failed to read WOPI request body. Please check the WOPI address.", err.Error())
		return err
	}

	// wopiData contains the XML from `<WOPI>/hosting/discovery`.
	var wopiData WopiDiscovery
	if err := xml.Unmarshal(body, &wopiData); err != nil {
		p.client.Log.Error("WOPI request error. Failed to unmarshal WOPI XML. Please check the WOPI address.", err.Error())
		return err
	}

	WopiFiles = make(map[string]WopiFile)
	for _, app := range wopiData.NetZone.App {
		for _, action := range app.Action {
			ext := strings.ToLower(action.Ext)
			if ext == "" || ext == "png" || ext == "jpg" || ext == "jpeg" || ext == "gif" {
				continue
			}
			WopiFiles[ext] = WopiFile{action.URLSrc, action.Name}
		}
	}

	p.client.Log.Info("WOPI file info loaded successfully!", "wopiFiles", WopiFiles)
	return nil
}
