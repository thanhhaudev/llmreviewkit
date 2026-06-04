;; mem_owner module — owns the shared memory and function table.
;; All other modules (env, grammar env) import from here.
;; This way, all modules share the same memory instance regardless of
;; how many env-like modules are instantiated.

(module
  ;; Shared memory: 512 pages initial (32MB), up to 32768 pages (2GB).
  (memory (export "memory") 512 32768)

  ;; Shared indirect function table for call_indirect.
  (table (export "__indirect_function_table") 1024 funcref)
)
