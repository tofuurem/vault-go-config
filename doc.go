// Package vltconfig loads typed Go configuration structs from process
// environment variables, HashiCorp Vault KV v2, JSON, and caller-provided
// defaults.
//
// Sources are merged by leaf with the priority Env, Vault, JSON, then existing
// destination values. Vault is skipped only when its access environment is
// entirely absent; configured Vault failures are strict by default.
package vltconfig
