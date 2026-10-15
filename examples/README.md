# Examples

Worked examples, written with this SDK. Each one makes real API calls. The Jev
examples read `TYPESAFE_API_KEY`; the Clef example reads `CLOUDFLARE_ACCOUNT_ID`
and `CLOUDFLARE_API_TOKEN`, a Workers AI token.

| Example | Source |
| --- | --- |
| [`triage`](triage) | The support-ticket walkthrough in the [quick start](https://docs.typesafe.ai/introduction/quickstart): one request, three questions, routing in code. |
| [`locate`](locate) | The [Box](https://docs.typesafe.ai/primitives/box) page: find something in a photo, check it is there with a Noul, and save a crop. Uses `jev-preview`. |
| [`screenshot`](screenshot) | Clef on Workers AI: triage a support message with a screenshot attached, sent as an image ahead of the state. |

```sh
export TYPESAFE_API_KEY=...
go run ./examples/triage
go run ./examples/locate kitchen.jpg "the coffee mug"

export CLOUDFLARE_ACCOUNT_ID=... CLOUDFLARE_API_TOKEN=...
go run ./examples/screenshot checkout.png "The checkout button does nothing."
```
