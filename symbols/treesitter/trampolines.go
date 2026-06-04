//go:build !lite

package treesitter

import (
	"context"
	"fmt"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// runtimeFns holds late-bound references to runtime exports that the
// grammar side modules import via env.wasm. Populated by bindRuntimeFns
// AFTER the tree-sitter runtime module instantiates and BEFORE any
// grammar loads.
//
// All fields are nil until bindRuntimeFns runs. The trampoline closures
// in addTrampolines read these by pointer, so they pick up the values
// once they're set.
type runtimeFns struct {
	calloc, malloc, free, realloc           api.Function
	memcpy, memcmp                          api.Function
	iswspace, iswxdigit, iswalnum, iswalpha api.Function
}

// bindRuntimeFns populates r from runtimeMod's exports. Returns an
// error if any required export is missing.
func bindRuntimeFns(r *runtimeFns, runtimeMod api.Module) error {
	exports := map[string]*api.Function{
		"calloc":    &r.calloc,
		"malloc":    &r.malloc,
		"free":      &r.free,
		"realloc":   &r.realloc,
		"memcpy":    &r.memcpy,
		"memcmp":    &r.memcmp,
		"iswspace":  &r.iswspace,
		"iswxdigit": &r.iswxdigit,
		"iswalnum":  &r.iswalnum,
		"iswalpha":  &r.iswalpha,
	}
	for name, ptr := range exports {
		fn := runtimeMod.ExportedFunction(name)
		if fn == nil {
			return fmt.Errorf("treesitter: runtime missing export %q", name)
		}
		*ptr = fn
	}
	return nil
}

// addTrampolines registers the 10 libc trampolines on the given host
// module builder. The closures capture rfns by pointer so they read
// the late-bound runtime function references at call time.
//
// The returned builder is the same builder with additional functions
// chained on — caller continues chaining or calls Instantiate.
func addTrampolines(b wazero.HostModuleBuilder, rfns *runtimeFns) wazero.HostModuleBuilder {
	return b.
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, nmemb, size int32) int32 {
			r, err := rfns.calloc.Call(ctx, uint64(nmemb), uint64(size))
			if err != nil {
				panic(fmt.Errorf("treesitter: calloc trampoline: %w", err))
			}
			return api.DecodeI32(r[0])
		}).
		Export("calloc").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, size int32) int32 {
			r, err := rfns.malloc.Call(ctx, uint64(size))
			if err != nil {
				panic(fmt.Errorf("treesitter: malloc trampoline: %w", err))
			}
			return api.DecodeI32(r[0])
		}).
		Export("malloc").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, ptr int32) {
			if _, err := rfns.free.Call(ctx, uint64(ptr)); err != nil {
				panic(fmt.Errorf("treesitter: free trampoline: %w", err))
			}
		}).
		Export("free").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, ptr, size int32) int32 {
			r, err := rfns.realloc.Call(ctx, uint64(ptr), uint64(size))
			if err != nil {
				panic(fmt.Errorf("treesitter: realloc trampoline: %w", err))
			}
			return api.DecodeI32(r[0])
		}).
		Export("realloc").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, dst, src, n int32) int32 {
			r, err := rfns.memcpy.Call(ctx, uint64(dst), uint64(src), uint64(n))
			if err != nil {
				panic(fmt.Errorf("treesitter: memcpy trampoline: %w", err))
			}
			return api.DecodeI32(r[0])
		}).
		Export("memcpy").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, a, b, n int32) int32 {
			r, err := rfns.memcmp.Call(ctx, uint64(a), uint64(b), uint64(n))
			if err != nil {
				panic(fmt.Errorf("treesitter: memcmp trampoline: %w", err))
			}
			return api.DecodeI32(r[0])
		}).
		Export("memcmp").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, c int32) int32 {
			r, err := rfns.iswspace.Call(ctx, uint64(c))
			if err != nil {
				panic(fmt.Errorf("treesitter: iswspace trampoline: %w", err))
			}
			return api.DecodeI32(r[0])
		}).
		Export("iswspace").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, c int32) int32 {
			r, err := rfns.iswxdigit.Call(ctx, uint64(c))
			if err != nil {
				panic(fmt.Errorf("treesitter: iswxdigit trampoline: %w", err))
			}
			return api.DecodeI32(r[0])
		}).
		Export("iswxdigit").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, c int32) int32 {
			r, err := rfns.iswalnum.Call(ctx, uint64(c))
			if err != nil {
				panic(fmt.Errorf("treesitter: iswalnum trampoline: %w", err))
			}
			return api.DecodeI32(r[0])
		}).
		Export("iswalnum").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, c int32) int32 {
			r, err := rfns.iswalpha.Call(ctx, uint64(c))
			if err != nil {
				panic(fmt.Errorf("treesitter: iswalpha trampoline: %w", err))
			}
			return api.DecodeI32(r[0])
		}).
		Export("iswalpha").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, assertion, file, line, function int32) {
			fmt.Fprintf(os.Stderr, "[treesitter] grammar __assert_fail(%d, %d, %d, %d)\n",
				assertion, file, line, function)
			os.Exit(3)
		}).
		Export("__assert_fail")
}
