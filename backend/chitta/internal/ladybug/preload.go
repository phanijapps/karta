package ladybug

// #cgo LDFLAGS: -ldl
// #include <stdlib.h>
// #include <dlfcn.h>
// int lbdb_preload_global(const char* path) {
//   return dlopen(path, RTLD_NOW | RTLD_GLOBAL) != NULL ? 0 : 1;
// }
import "C"

import (
	"unsafe"

	"zbot/backend/internal/domain"
)

// preloadGlobal dlopens a shared library with RTLD_NOW|RTLD_GLOBAL after
// liblbug.so is loaded into the process. The engine's extension loader
// then finds the library resident and its engine symbols resolved —
// without this, the engine's plain dlopen is non-deterministic against
// the vendored shared liblbug (glibc static-TLS/symbol timing).
func preloadGlobal(path string) error {
	cstr := C.CString(path)
	defer C.free(unsafe.Pointer(cstr))
	if C.lbdb_preload_global(cstr) != 0 {
		return domain.NewChittaError(domain.ErrUnavailable,
			"preload %s: dlopen failed (missing engine symbols or file)", path)
	}
	return nil
}
