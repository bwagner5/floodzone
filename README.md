# floodzone

floodzone is a CLI tool to create and populate a Route 53 Private Hosted Zone with a bunch of resource record sets for testing purposes. 

## Description

floodzone can create private hosted zones or populate existing private hosted zones with resource record sets. The default resource record set is an A record with 1 value of `127.0.0.1` and a TTL of 300 seconds. Floodzone creates resource record sets in batches with a configurable sleep in-between to scale-up resource record sets more gradually.  

## Usage:

```
> floodzone --help
Usage of floodzone:
  -batch-delay-duration duration
    	Duration of time between batch executions (default 10s)
  -delete
    	Delete records
  -endpoint string
    	Route 53 API endpoint to use
  -hosted-zone-id string
    	Hosted Zone ID
  -max-batch-size int
    	Max batch size of resource record set creations in one API call (max is 1,000) (default 100)
  -region string
    	AWS Region
  -total-records int
    	Total resource record sets in the hosted zone (max is 10,000) (default 1000)
  -vpc-id string
    	VPC ID to associate the PHZ with if it doesn't already exist
```

## Examples:

### Fill up an existing hosted zone with 500 resource record sets
```
> floodzone --hosted-zone-id <ID> --total-records 500

```

### Create and flood a new private hosted zone with 500 resource record sets
```
> floodzone --total-records 500 --vpc-id <VPC_ID>
```

### Delete 10 resource record sets after flooding

```
> floodzone --delete --total-records 10 --hosted-zone-id <ID>
```

### Delete the zone by deleting all the resource record sets

```
> floodzone --delete --total-records 500 --hosted-zone-id <ID>
```
