package modules

type ModuleConfig struct {
	ListenAddress string `mapstructure:"listen_addr"`

	// RegisterAddress is address others can find this module
	// In some environment, especially in cloud, listen address and register address are different
	// It will use listen address if register address is empty
	RegisterAddress string `mapstructure:"register_addr"`

	// Metadata that should be saved in etcd
	//
	// grpc resolver is used to save address and metadata to etcd. Due to current implementation
	// in grpc and etcd, metadata is used as key of map, so metadata can not be type that can not
	// be hashed, such as map. If you want to store complex metadata, consider store it as marshalled
	// json string. This constrain may be releaxed in future version of grpc
	Metadata interface{}

	// EnableTLS enable tls for this module
	EnableTLS bool `mapstructure:"enable_tls"`

	// CAFile is root cert file path
	//  combined use with EnableClientAuth
	// Deprecated: use ClientCAFile instead
	CAFile string `mapstructure:"ca_file"`

	// CertFile is cert file path
	CertFile string `mapstructure:"cert_file"`

	// KeyFile is key file path
	KeyFile string `mapstructure:"key_file"`

	// EnableClientAuth enable tls client authentication support at server
	//  ref to https://blog.cloudflare.com/introducing-tls-client-auth/
	EnableClientAuth bool `mapstructure:"enable_client_auth"`

	// ClientCAFile is ca file path
	//  that servers use if required to verify a client certificate
	//  combined use with EnableClientAuth
	ClientCAFile string `mapstructure:"client_ca_file"`
}

