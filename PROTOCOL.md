# FocusLynx protocol

Commands are ASCII wrapped in angle brackets, e.g. `<F1GETSTATUS>`; the destination is
`F1`/`F2` (focuser channel) or `FH` (hub).

Successful replies start with a `!` acknowledgement:

- an action adds a one-word status line: `M` (moving), `H` (homing), `HALTED`,
  `STOPPED`, or `SET` (config saved);
- a query adds a `key = value` block (after a `STATUSn` / `CONFIGn` / `HUB INFO`
  header) terminated by `END`.

Rejected commands return `ER=<n> <message>` before or after the acknowledgement.

| Command | Meaning | Reply |
|---|---|---|
| `<F1GETSTATUS>` | status block | `Temp(C)`, `Curr Pos`, `Targ Pos`, `IsMoving`, `IsHoming`, `IsHomed`, `TmpProbe`, … `END` |
| `<F1GETCONFIG>` | config block | `Nickname`, `Max Pos`, `Dev Typ`, `TComp ON`, `TempCo A`–`E`, `TC Mode`, `BLC En`, `BLC Stps`, `LED Brt`, `TC@Start` `END` |
| `<FHGETHUBINFO>` | hub info | `Hub FVer`, `Sleeping`, `Wired IP`, `WF …` `END` |
| `<F1HELLO>` | report nickname | `!` + nickname |
| `<F1MA012345>` | move absolute (6-digit) | `!` `M` |
| `<F1MIR0>` `<F1MOR1>` `<F1ERM>` | relative move in/out (`z`: 0 high / 1 low), end relative | `!` `M` / `STOPPED` |
| `<F1HALT>` `<F1HOME>` `<F1CENTER>` | halt / home / center-of-travel | `!` `HALTED` / `H` / `M` |
| `<F1SCNN…>` `<F1SCDT**>` `<F1SCCP012345>` | set nickname / device type / sync position | `!` `SET` |
| `<F1SCBE1>` `<F1SCBS50>` | backlash comp enable / steps (2-digit) | `!` `SET` |
| `<F1SCTE1>` `<F1SCTMC>` `<F1SCTCD+0092>` `<F1SCTS1>` | temp-comp enable / mode A–E / coefficient / at-start | `!` `SET` |
| `<FHSCLB085>` | hub LED brightness (3-digit, 0–100) | `!` `SET` |
| `<F1RESET>` | factory reset channel | `!` `SET` |
