package plugin

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
)

// this implements geth service
type PluginManager struct {
	nodeName      string // geth node name
	pluginBaseDir string // base directory for all the plugins
	verifier      Verifier
	centralClient *CentralClient
	downloader    *Downloader
	settings      *Settings
	mux           sync.Mutex
	plugins       map[PluginInterfaceName]managedPlugin
}

func (s *PluginManager) Protocols() []p2p.Protocol { return nil }

func (s *PluginManager) APIs() []rpc.API {
	// the below code show how to expose APIs of a pluggin via JSON RPC
	// this is only for demonstration purposes
	helloWorldAPI := make([]rpc.API, 0)
	helloWorldPluginTemplate := new(HellowWorldPluginTemplate)
	if err := s.GetPluginTemplate(HelloWorldPluginInterfaceName, helloWorldPluginTemplate); err != nil {
		log.Info("plugin: not configured", "name", HelloWorldPluginInterfaceName, "err", err)
	} else {
		pluginInstance, err := helloWorldPluginTemplate.Get()
		if err != nil {
			log.Info("plugin: instance not ready", "name", HelloWorldPluginInterfaceName, "err", err)
		} else {
			helloWorldAPI = append(helloWorldAPI, rpc.API{
				Namespace: fmt.Sprintf("plugin[%s]", HelloWorldPluginInterfaceName),
				Service:   pluginInstance,
				Version:   "1.0",
				Public:    true,
			})
		}
	}
	return append([]rpc.API{
		{
			Namespace: "admin",
			Service:   NewPluginManagerAPI(s),
			Version:   "1.0",
			Public:    false,
		},
	}, helloWorldAPI...)
}

func (s *PluginManager) Start(_ *p2p.Server) (err error) {
	log.Info("Starting all plugins", "count", len(s.plugins))
	startedPlugins := make([]managedPlugin, 0, len(s.plugins))
	for _, p := range s.plugins {
		if err = p.Start(); err != nil {
			break
		} else {
			startedPlugins = append(startedPlugins, p)
		}
	}
	if err != nil {
		for _, p := range startedPlugins {
			_ = p.Stop()
		}
	}
	return
}

func (s *PluginManager) getPlugin(name PluginInterfaceName) (managedPlugin, bool) {
	s.mux.Lock()
	defer s.mux.Unlock()
	p, ok := s.plugins[name]
	return p, ok
}

// store the plugin instance to the value of the pointer v and cache it
// this function makes sure v value will never be nil
func (s *PluginManager) GetPluginTemplate(name PluginInterfaceName, v managedPlugin) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("invalid argument value, expected a pointer but got %s", reflect.TypeOf(v))
	}
	recoverToErrorFunc := func(f func()) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("%s", r)
			}
		}()
		f()
		return
	}
	if p, ok := s.plugins[name]; ok {
		return recoverToErrorFunc(func() {
			rv.Elem().Set(reflect.ValueOf(p))
		})
	}
	pluginDefinition, ok := s.settings.GetPluginDefinition(name)
	if !ok {
		return fmt.Errorf("no plugin definition for %s", name)
	}
	pluginProvider, ok := pluginProviders[name]
	if !ok {
		return fmt.Errorf("plugin %s not supported", name)
	}
	base, err := newBasePlugin(s, name, pluginDefinition, pluginProvider)
	if err != nil {
		return fmt.Errorf("plugin [%s] %s", name, err.Error())
	}
	if err := recoverToErrorFunc(func() {
		rv.Elem().FieldByName("BasePlugin").Set(reflect.ValueOf(base))
	}); err != nil {
		return err
	}
	s.mux.Lock()
	defer s.mux.Unlock()
	s.plugins[name] = v
	return nil
}

func (s *PluginManager) Stop() error {
	log.Info("Stopping all plugins", "count", len(s.plugins))
	allErrors := make([]error, 0)
	for _, p := range s.plugins {
		if err := p.Stop(); err != nil {
			allErrors = append(allErrors, err)
		}
	}
	log.Info("All plugins stopped", "errors", allErrors)
	if len(allErrors) == 0 {
		return nil
	}
	return fmt.Errorf("%s", allErrors)
}

// Provide details of current plugins being used
func (s *PluginManager) PluginsInfo() interface{} {
	info := make(map[PluginInterfaceName]interface{})
	if len(s.plugins) == 0 {
		return info
	}
	info["baseDir"] = s.pluginBaseDir
	for _, p := range s.plugins {
		k, v := p.Info()
		info[k] = v
	}
	return info
}

func NewPluginManager(nodeName string, settings *Settings, skipVerify bool, localVerify bool, publicKey string) (*PluginManager, error) {
	pm := &PluginManager{
		nodeName:      nodeName,
		pluginBaseDir: settings.BaseDir.String(),
		centralClient: NewPluginCentralClient(settings.CentralConfig),
		plugins:       make(map[PluginInterfaceName]managedPlugin),
		settings:      settings,
	}
	pm.downloader = NewDownloader(pm)
	if skipVerify {
		log.Warn("plugin: ignore integrity verification")
		pm.verifier = NewNonVerifier()
	} else {
		var err error
		if pm.verifier, err = NewVerifier(pm, localVerify, publicKey); err != nil {
			return nil, err
		}
	}
	return pm, nil
}

func NewEmptyPluginManager() *PluginManager {
	return &PluginManager{
		plugins: make(map[PluginInterfaceName]managedPlugin),
	}
}
