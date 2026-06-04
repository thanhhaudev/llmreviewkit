;; GOT.mem module — Global Offset Table for memory addresses.
;; Provides static address values that emscripten dynamic-linking code expects.

(module
  ;; __stack_low: low end of stack (where stack overflow check fires).
  (global (export "__stack_low") (mut i32) (i32.const 65536))
  ;; __stack_high: initial stack pointer (top of stack, grows down).
  (global (export "__stack_high") (mut i32) (i32.const 1048576))
  ;; __heap_base: start of the heap (after stack + data segments).
  (global (export "__heap_base") (mut i32) (i32.const 5242880))
)
