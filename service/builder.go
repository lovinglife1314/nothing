package service

type Builder struct {
	options []Option
	modules []moduleInfo
}

func WithModule(provider ModuleProvider, options ...ModuleOption ) *Builder {
	b := Builder{}
	return b.WithModule(provider, options...)
}

func (b *Builder) Build() (Host, error) {
	return newHost(*b)
}

func (b * Builder) WithOption(options ... Option) *Builder {
	b.options= append(b.options, options...)
	return b
}
func (b *Builder) WithModule(provider ModuleProvider, options ...ModuleOption ) *Builder{
	b.modules = append(b.modules, moduleInfo{provider, options})
	return b
}


type moduleInfo struct {
	provider ModuleProvider
	options  []ModuleOption
}