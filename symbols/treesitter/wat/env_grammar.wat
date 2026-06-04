;; env_grammar module — same as env.wat but with __memory_base = 11712.
;; Used for grammar side modules so their data segments don't overlap
;; with the runtime's data at address 0.
;; Memory and table are imported from "mem_owner" (same instance as env),
;; so all modules share the same memory.

(module
  ;; Import shared memory and table from mem_owner.
  (import "mem_owner" "memory" (memory 0))
  (import "mem_owner" "__indirect_function_table" (table 0 funcref))

  ;; Runtime-needed callbacks
  (import "host" "emscripten_resize_heap" (func $resize (param i32) (result i32)))
  (import "host" "_abort_js" (func $abort))
  (import "host" "tree_sitter_query_progress_callback" (func $qprog (param i32) (result i32)))
  (import "host" "tree_sitter_progress_callback" (func $prog (param i32 i32) (result i32)))
  (import "host" "tree_sitter_parse_callback" (func $parse (param i32 i32 i32 i32 i32)))
  (import "host" "tree_sitter_log_callback" (func $log (param i32 i32)))

  ;; Grammar-needed libc functions (forwarded by host trampolines to runtime exports)
  (import "host" "calloc"       (func $calloc       (param i32 i32) (result i32)))
  (import "host" "malloc"       (func $malloc       (param i32) (result i32)))
  (import "host" "free"         (func $free         (param i32)))
  (import "host" "realloc"      (func $realloc      (param i32 i32) (result i32)))
  (import "host" "memcpy"       (func $memcpy       (param i32 i32 i32) (result i32)))
  (import "host" "memcmp"       (func $memcmp       (param i32 i32 i32) (result i32)))
  (import "host" "iswspace"     (func $iswspace     (param i32) (result i32)))
  (import "host" "iswxdigit"    (func $iswxdigit    (param i32) (result i32)))
  (import "host" "iswalnum"     (func $iswalnum     (param i32) (result i32)))
  (import "host" "iswalpha"     (func $iswalpha     (param i32) (result i32)))
  (import "host" "__assert_fail" (func $assfail     (param i32 i32 i32 i32)))

  (global (export "__stack_pointer") (mut i32) (i32.const 65536))
  ;; __memory_base = 11712: grammar data starts just past the runtime's
  ;; 11676-byte data region (rounded up to 64-byte alignment).
  (global (export "__memory_base") i32 (i32.const 11712))
  ;; __table_base = 30: grammar table entries start after the runtime's
  ;; 30 table entries so they don't overwrite the runtime's function table.
  ;; The mem_owner table has 1024 entries (30 runtime + 994 for grammars).
  (global (export "__table_base") i32 (i32.const 30))

  ;; Re-export memory and table from mem_owner.
  (export "memory" (memory 0))
  (export "__indirect_function_table" (table 0))

  (export "emscripten_resize_heap" (func $resize))
  (export "_abort_js" (func $abort))
  (export "tree_sitter_query_progress_callback" (func $qprog))
  (export "tree_sitter_progress_callback" (func $prog))
  (export "tree_sitter_parse_callback" (func $parse))
  (export "tree_sitter_log_callback" (func $log))

  (export "calloc" (func $calloc))
  (export "malloc" (func $malloc))
  (export "free" (func $free))
  (export "realloc" (func $realloc))
  (export "memcpy" (func $memcpy))
  (export "memcmp" (func $memcmp))
  (export "iswspace" (func $iswspace))
  (export "iswxdigit" (func $iswxdigit))
  (export "iswalnum" (func $iswalnum))
  (export "iswalpha" (func $iswalpha))
  (export "__assert_fail" (func $assfail))
)
