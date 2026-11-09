// Package devin adapts Devin's ACP (Agent Client Protocol) subprocess to the
// shared agent backend contract. One `devin acp` process hosts every Devin
// session: JSON-RPC over newline-delimited stdio, authenticated per process.
//
// Mapping: ACP session ↔ Fuyu thread, ACP prompt ↔ Fuyu turn (turn identity is
// allocated locally; ACP does not return one), session/update notifications ↔
// item events, session/request_permission ↔ approval interactions, and the
// session config options (model, thought_level, speed, mode) ↔ model, effort,
// service tier and permission mode.
package devin
