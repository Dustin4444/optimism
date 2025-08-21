//go:build go1.25

// Disable annotating anonymous memory mappings. Cannon doesn't support this syscall
// Only applies on go1.25 and above so this file is conditionally included.
//
//go:debug decoratemappings=0
package main
