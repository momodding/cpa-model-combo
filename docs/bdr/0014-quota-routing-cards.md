# BDR 0014: Quota routing cards

Amends BDR 0013.

- The Routing diagram section shows one collapsible card per configured model combo instead of one shared canvas. The header shows the combo name, its strategy (same labels as the Configuration tab), and the outcome of its most recent request: Succeeded, Failed, Pending, or No requests yet, with the time. Status is a coloured dot plus text, never colour alone.
- Only the first card is open when the tab first loads. Open and closed choices are kept while the page stays open and reset on reload.
- Each open card has its own diagram and its own Fit button. Selecting a node shows its details and timeline inside that card; closing the card clears the selection.
- The request picker is unchanged. Choosing a request shows it on the card of the route it belongs to; other cards keep showing their own latest request.
- Polling cadence, event retention, the text fallback and the empty-state message are unchanged.
