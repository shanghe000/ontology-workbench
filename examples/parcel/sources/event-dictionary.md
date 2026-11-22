# Event dictionary

## Delivery confirmed

The first proof-of-delivery event provides the actual delivery timestamp. Deduplicate repeated scans by parcel ID.

## Carrier accepted

The earliest carrier-accepted scan closes the handoff interval. Pair it with the latest preceding parcel-ready event.
