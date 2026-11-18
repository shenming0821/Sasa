#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

typedef void *sasa_terminal_t;
const char *sasa_terminal_load(const char *, const char *);
sasa_terminal_t sasa_terminal_new(uintptr_t, uintptr_t, const char *, double, double, double, double, bool);
const char *sasa_terminal_error(void);
void sasa_terminal_present(sasa_terminal_t, double, double, double, double, bool, bool);
void sasa_terminal_hide(sasa_terminal_t);
void sasa_terminal_free(sasa_terminal_t);
bool sasa_terminal_exited(sasa_terminal_t);
void sasa_terminal_text(sasa_terminal_t, const char *);
