# Runbook: Onboarding support

For IT admins and operators helping users through `https://<tenant>.vpn.malgary.com/setup`.

| Symptom | Cause | Fix |
| --- | --- | --- |
| Sign-in page says "NetBird", not "Malgary" | App still points at NetBird's cloud | Run `netbird down`, then `netbird up --management-url https://<tenant>.vpn.malgary.com:443`; on mobile, Change server |
| "User not allowed" / account refused | Personal account, or user not imported | Use the work account; owner or tenant admin imports the user (`malgaryctl users import`) |
| Sign-in loops or "token not yet valid" | Device clock wrong | Turn on automatic date and time, retry |
| Connected, but egress IP isn't the company IP | Routing rule for that user is "destinations only", or the routing peers haven't joined | Check the user's effective routing rule; `malgaryctl stack reconcile <tenant>` |
| Device blocked after connecting | Posture rule (OS or app too old) | Update the OS or NetBird app; the block lifts within a minute |
| Seat limit message on import | Subscription quantity reached | Owner raises quantity (`malgaryctl subscription change`) |

Record every new symptom here during the pilot (SC-001 tracks unaided onboarding).
