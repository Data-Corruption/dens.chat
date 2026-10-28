# Spikes

Throwaway experiments that answer a design question before the code that depends on it. Each is a directory in this Go module, whose dependencies never reach the product, and each goes once its findings are in the design docs. The module runs Dens's own seams from this tree where they exist (`replace` in `go.mod`), so what passes in a spike passes for Dens.

- [`android/`](android/README.md): the Android form, before M12 builds it.
