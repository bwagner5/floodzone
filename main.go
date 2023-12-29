package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/google/uuid"
)

type Zone struct {
	R53 *route53.Client
}

type Options struct {
	MaxBatchSize int
	TotalRecords int
	HostedZoneID string
	BatchDelay   time.Duration
	VPCID        string
	Delete       bool
	Endpoint     string
}

func main() {
	ctx := context.Background()
	opts := Options{}
	flag.IntVar(&opts.MaxBatchSize, "max-batch-size", 100, "Max batch size of resource record set creations in one API call (max is 1,000)")
	flag.IntVar(&opts.TotalRecords, "total-records", 1_000, "Total resource record sets in the hosted zone (max is 10,000)")
	flag.StringVar(&opts.HostedZoneID, "hosted-zone-id", "", "Hosted Zone ID")
	flag.DurationVar(&opts.BatchDelay, "batch-delay-duration", 10*time.Second, "Duration of time between batch executions")
	flag.StringVar(&opts.VPCID, "vpc-id", "", "VPC ID to associate the PHZ with if it doesn't already exist")
	flag.BoolVar(&opts.Delete, "delete", false, "Delete records")
	flag.StringVar(&opts.Endpoint, "endpoint", "", "Route 53 API endpoint to use")
	// region should only be used in the client config, so don't add to Options struct
	region := flag.String("region", "", "AWS Region")
	flag.Parse()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if opts.Endpoint != "" {
		cfg.BaseEndpoint = &opts.Endpoint
	}
	if *region != "" {
		cfg.Region = *region
	}
	r53 := route53.NewFromConfig(cfg)
	zone := Zone{R53: r53}

	// Create a hosted zone if no hosted zone ID passed in by user
	if opts.HostedZoneID == "" {
		if opts.VPCID == "" {
			fmt.Println("--vpc-id is required when --hosted-zone-id is not provided.")
			os.Exit(1)
		}
		zoneID, err := zone.CreatePrivateHostedZone(ctx, opts.VPCID, cfg.Region)
		if err != nil {
			log.Fatalf("unable to create hosted zone: %s", err)
		}
		opts.HostedZoneID = zoneID
		log.Printf("✅ Successfully Created Hosted Zone \"%s\" to flood 🌊!", zoneID)
	}

	// Describe and Pretty Print Hosted Zone to stdout
	hz, err := r53.GetHostedZone(ctx, &route53.GetHostedZoneInput{Id: &opts.HostedZoneID})
	if err != nil {
		log.Fatalf("unable to describe hosted zone: %s", err)
	}
	rrCount := int(*hz.HostedZone.ResourceRecordSetCount)

	hzPretty, err := json.MarshalIndent(hz.HostedZone, "", "    ")
	if err != nil {
		log.Fatalf("unable to pretty print hosted zone: %s", err)
	}
	fmt.Println(string(hzPretty))

	// Create
	if !opts.Delete {
		if err := zone.CreateResourceRecordSets(ctx, hz.HostedZone, rrCount, opts.TotalRecords, opts.MaxBatchSize, opts.BatchDelay); err != nil {
			log.Fatalf("Error when creating resource record sets: %s", err)
		}
	} else {
		remainingRRS, err := zone.DeleteResourceRecordSets(ctx, hz.HostedZone, opts.MaxBatchSize, opts.TotalRecords, opts.BatchDelay)
		if err != nil {
			log.Fatalf("Error when deleting resource record sets: %s", err)
		}
		// if there are no remaining resource record sets, delete the zone too
		if remainingRRS == 0 {
			if _, err := zone.R53.DeleteHostedZone(ctx, &route53.DeleteHostedZoneInput{Id: &opts.HostedZoneID}); err != nil {
				log.Fatalf("Error when deleting the zone %s: %s", opts.HostedZoneID, err)
			}
			log.Printf("✅ Successfully deleted the private hosted zone %s since all record sets were deleted.", opts.HostedZoneID)
		}
	}

	log.Printf("✅✅ DONE ✅✅")
}

// CreateHostedZone creates a private hosted zone with an unique name in the format: floodzone-test-<UUID>.aws
// The hosted zone ID is returned.
func (z Zone) CreatePrivateHostedZone(ctx context.Context, vpcID string, region string) (string, error) {
	hzOut, err := z.R53.CreateHostedZone(ctx, &route53.CreateHostedZoneInput{
		Name:            aws.String(fmt.Sprintf("floodzone-test-%s.aws", uuid.NewString())),
		CallerReference: aws.String(fmt.Sprint(time.Now().Unix())),
		HostedZoneConfig: &types.HostedZoneConfig{
			PrivateZone: true,
			Comment:     aws.String(fmt.Sprintf("Created by floodzone at %s", time.Now().UTC())),
		},
		VPC: &types.VPC{
			VPCId:     aws.String(vpcID),
			VPCRegion: types.VPCRegion(region),
		},
	})
	if err != nil {
		return "", err
	}
	return *hzOut.HostedZone.Id, err
}

// DeleteResourceRecordSets deletes the desired number of Resource Record Sets in controlled batches and returns the
// remaining resource record sets in the zone excluding SOA and NS records.
func (z Zone) DeleteResourceRecordSets(ctx context.Context, hostedZone *types.HostedZone, maxBatchSize int, desiredDeletions int, batchDelay time.Duration) (int, error) {
	rrs, err := z.ListResourceRecordSets(ctx, hostedZone, maxBatchSize)
	if err != nil {
		return 0, err
	}
	currentRRS := len(rrs)
	deletedRecords := 0
	totalRecordsToDelete := len(rrs)
	if desiredDeletions < len(rrs) {
		totalRecordsToDelete = desiredDeletions
	}
	for deletedRecords < totalRecordsToDelete {
		var changes []types.Change
		for i := 0; i < len(rrs) && i < maxBatchSize; i++ {
			changes = append(changes, types.Change{
				Action:            types.ChangeActionDelete,
				ResourceRecordSet: &rrs[i],
			})
		}
		_, err := z.R53.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: hostedZone.Id,
			ChangeBatch: &types.ChangeBatch{
				Changes: changes,
			},
		})
		if err != nil {
			return 0, err
		}
		rrs = rrs[len(changes):]
		deletedRecords += len(changes)
		log.Printf("✅ Executed batch of %d Delete Resource Record Sets on %s   %d/%d  - Sleeping for %s\n", len(changes), *hostedZone.Id, deletedRecords, totalRecordsToDelete, batchDelay)
		if deletedRecords != totalRecordsToDelete {
			time.Sleep(batchDelay)
		}
	}
	return currentRRS - totalRecordsToDelete, nil
}

func (z Zone) ListResourceRecordSets(ctx context.Context, hostedZone *types.HostedZone, maxBatchSize int) ([]types.ResourceRecordSet, error) {
	var rrs []types.ResourceRecordSet
	var nextRecordName *string
	for {
		rrsOut, err := z.R53.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
			HostedZoneId:    hostedZone.Id,
			MaxItems:        aws.Int32(int32(maxBatchSize)),
			StartRecordName: nextRecordName,
		})
		if err != nil {
			return rrs, err
		}
		for _, rr := range rrsOut.ResourceRecordSets {
			if rr.Type == types.RRTypeSoa || rr.Type == types.RRTypeNs {
				continue
			}
			rrs = append(rrs, rr)
		}
		if !rrsOut.IsTruncated {
			break
		}
		nextRecordName = rrsOut.NextRecordName
	}
	return rrs, nil
}

func (z Zone) CreateResourceRecordSets(ctx context.Context, hostedZone *types.HostedZone,
	currentRRSetCount int, desiredRecords int, maxBatchSize int, batchDelay time.Duration) error {
	for currentRRSetCount < desiredRecords {
		batchSize := maxBatchSize
		if (desiredRecords - currentRRSetCount) < maxBatchSize {
			batchSize = desiredRecords - currentRRSetCount
		}
		_, err := z.R53.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: hostedZone.Id,
			ChangeBatch: &types.ChangeBatch{
				Changes: createChangeBatch(*hostedZone.Name, batchSize),
			},
		})
		if err != nil {
			return err
		}
		currentRRSetCount += batchSize
		log.Printf("✅ Executed batch of %d Create Resource Record Sets on %s. %d/%d  - Sleeping for %s\n", batchSize, *hostedZone.Id, currentRRSetCount, desiredRecords, batchDelay)
		if currentRRSetCount != desiredRecords {
			time.Sleep(batchDelay)
		}
	}
	return nil
}

func createChangeBatch(hzName string, batchSize int) []types.Change {
	var changes []types.Change
	for i := 0; i < batchSize; i++ {
		changes = append(changes, types.Change{
			Action: types.ChangeActionCreate,
			ResourceRecordSet: &types.ResourceRecordSet{
				Name: aws.String(fmt.Sprintf("%s.%s", uuid.NewString(), hzName)),
				Type: types.RRTypeA,
				TTL:  aws.Int64(300),
				ResourceRecords: []types.ResourceRecord{
					{
						Value: aws.String("127.0.0.1"),
					},
				},
			},
		})
	}
	return changes
}
