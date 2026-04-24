package lang

import "path/filepath"

// binary extensions — return nil (skip entirely)
var binaryExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
	".ico": true, ".webp": true, ".bmp": true, ".tiff": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true,
	".pdf": true, ".ttf": true, ".woff": true, ".woff2": true, ".eot": true,
	".mp3": true, ".mp4": true, ".wav": true, ".ogg": true,
	".db": true, ".sqlite": true,
}

var registry []Extractor

func init() {
	registry = []Extractor{
		&GoExtractor{},
		&PythonExtractor{},
		&TypeScriptExtractor{},
		&RustExtractor{},
		&JavaExtractor{},
		&CSharpExtractor{},
		&KotlinExtractor{},
		&SwiftExtractor{},
		&ScalaExtractor{},
		&PHPExtractor{},
	}
}

// Detect returns the appropriate Extractor for a file path.
// Returns nil for binary files (should be skipped).
// Returns GenericExtractor for unknown text file types.
func Detect(path string) Extractor {
	ext := filepath.Ext(path)
	if binaryExts[ext] {
		return nil
	}
	for _, e := range registry {
		for _, x := range e.Extensions() {
			if x == ext {
				return e
			}
		}
	}
	return &GenericExtractor{}
}
