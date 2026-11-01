#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

typedef void *fuyu_terminal_t;
const char *fuyu_terminal_load(const char *, const char *);
fuyu_terminal_t fuyu_terminal_new(uintptr_t, uintptr_t, const char *, double, double, double, double, bool);
const char *fuyu_terminal_error(void);
void fuyu_terminal_present(fuyu_terminal_t, double, double, double, double, bool, bool);
void fuyu_terminal_hide(fuyu_terminal_t);
void fuyu_terminal_free(fuyu_terminal_t);
bool fuyu_terminal_exited(fuyu_terminal_t);
void fuyu_terminal_text(fuyu_terminal_t, const char *);
