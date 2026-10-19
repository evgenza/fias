package schema

// GARFormat связывает схему Ent с форматом XML для генерации пакетного импортёра.
type GARFormat struct {
	Kind string
}

func (GARFormat) Name() string { return "GARFormat" }
