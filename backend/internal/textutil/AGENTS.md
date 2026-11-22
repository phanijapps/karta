# textutil

One function: `NormalizeText` — the chitta text normalization (NFC fold,
lowercase, whitespace collapse) shared by the service layer, the
LadybugDB driver, and the fake driver. Lives outside `internal/domain`
so the domain package stays stdlib-pure. No other responsibilities; do
not grow this package.
