# Spikes

Throwaway experiments that answer a design question before the code that depends on it. Each is a directory in this Go module, whose dependencies never reach the product, and each goes once its findings are in `docs/dev/design.md`. The module runs Dens's own seams from this tree where they exist (`replace` in `go.mod`), so what passes in a spike passes for Dens.

- [`flatpak/`](flatpak/README.md): the Flatpak form, before M9.3 builds it.
