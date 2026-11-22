# BDR 0011: Quota polling works after you open Model Combo

Operators no longer set a separate management key: opening Model Combo with a valid CPA management session enables quota polling until CPA restarts (then open it again, or set `MODEL_COMBO_MANAGEMENT_KEY` for unattended start). Unsupported providers say so instead of "management key missing". The Quota tracking tab shows one card per account in the Auth Files style, with an actionable page-level notice when polling is blocked; polling settings are in a collapsible section. Unknown quota is still never shown as a measurement.
