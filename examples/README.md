# Examples

Worked examples from the TypeSafe documentation, written with this SDK. Each
one reads `TYPESAFE_API_KEY` from the environment and makes real API calls.

| Example | Source |
| --- | --- |
| [`triage`](triage) | The support-ticket walkthrough in the [quick start](https://docs.typesafe.ai/introduction/quickstart): one request, three questions, routing in code. |
| [`locate`](locate) | The [Box](https://docs.typesafe.ai/primitives/box) page: find something in a photo, check it is there with a Noul, and save a crop. Uses `jev-preview`. |

```sh
export TYPESAFE_API_KEY=...
go run ./examples/triage
go run ./examples/locate kitchen.jpg "the coffee mug"
```
