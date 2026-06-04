//go:build !lite

package treesitter

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func TestRuntime_EnvAndGOTMem_Instantiate(t *testing.T) {
	ctx := context.Background()
	// Build a fresh wazero runtime separately (not the singleton) so this
	// test exercises the mem_owner + env + GOT.mem path in isolation.
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		t.Fatal(err)
	}
	rfns := &runtimeFns{}
	if err := instantiateHostModule(ctx, rt, rfns); err != nil {
		t.Fatal(err)
	}
	// mem_owner must be instantiated before env (env imports memory/table from it).
	if _, err := rt.InstantiateWithConfig(ctx, MemOwnerWASM,
		wazero.NewModuleConfig().WithName("mem_owner")); err != nil {
		t.Fatalf("mem_owner: %v", err)
	}
	if _, err := rt.InstantiateWithConfig(ctx, EnvWASM,
		wazero.NewModuleConfig().WithName("env")); err != nil {
		t.Fatalf("env: %v", err)
	}
	if _, err := rt.InstantiateWithConfig(ctx, GOTMemWASM,
		wazero.NewModuleConfig().WithName("GOT.mem")); err != nil {
		t.Fatalf("GOT.mem: %v", err)
	}
}

func TestRuntime_Init_SmokeOnly(t *testing.T) {
	ctx := context.Background()
	rt, err := getRuntime(ctx)
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	if rt == nil {
		t.Fatal("getRuntime returned nil runtime")
	}
	// Verify singleton — second call returns same instance.
	rt2, err := getRuntime(ctx)
	if err != nil {
		t.Fatalf("getRuntime second call: %v", err)
	}
	if rt2 != rt {
		t.Fatal("getRuntime did not return singleton")
	}
}

func TestRuntime_FullInit(t *testing.T) {
	ctx := context.Background()
	r, err := getRuntime(ctx)
	if err != nil {
		t.Fatalf("getRuntime: %v", err)
	}
	if r.tsMod == nil {
		t.Fatal("tsMod nil — runtime not instantiated")
	}
	if r.rfns.malloc == nil {
		t.Fatal("rfns.malloc nil — bindRuntimeFns not called")
	}
	// Smoke: call malloc through the runtime to confirm late-binding works.
	res, err := r.rfns.malloc.Call(ctx, 128)
	if err != nil {
		t.Fatalf("malloc(128): %v", err)
	}
	if api.DecodeI32(res[0]) == 0 {
		t.Fatal("malloc(128) returned NULL")
	}
}
