package archexport

import (
	"slices"
	"strconv"
	"strings"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim/pricing"
)

// tfGen holds one Terraform export.
type tfGen struct {
	t       *tree
	w       hclWriter
	used    map[string]int
	name    map[string]string   // node or box ID → resource name
	vpcIdx  map[string]int      // VPC box ID → 10.<i>.0.0/16
	subnets map[string][]string // VPC box ID → AZ box IDs with a subnet
	azVPC   map[string]string   // AZ box ID → its VPC box ID
}

func isAWSNode(n *archdoc.Node) bool {
	switch n.Kind {
	case archdoc.KindClient, archdoc.KindExternal:
		return false
	}
	return n.Provider != string(pricing.GCP) && n.Provider != string(pricing.Azure)
}

// Terraform writes an AWS skeleton of the design (spec C2.11): opt.Header as
// comments, the AWS provider with var.region (default: the first region box
// that is a known AWS region), a VPC with a TODO security group per VPC box, a
// subnet per AZ box, then per node an ALB with target group and listener,
// launch template + autoscaling group (API, worker), ElastiCache, RDS (replicas
// with replicate_source_db), SQS, S3, OpenSearch or CloudFront. Secrets, the
// AMI and the DB user are variables without defaults. Labels only appear in
// tags (hclString); resource names come from tfIdent. GCP and Azure nodes
// become TODO comments.
func Terraform(d *archdoc.Design, opt Options) []byte {
	g := &tfGen{t: newTree(d), used: map[string]int{}, name: map[string]string{}, vpcIdx: map[string]int{},
		subnets: map[string][]string{}, azVPC: map[string]string{}}
	g.plan()
	w := &g.w
	for _, h := range headerLines(opt.Header) {
		w.line(strings.TrimRight("# "+h, " "))
	}
	if len(opt.Header) > 0 {
		w.line("")
	}
	w.open("terraform")
	w.open("required_providers")
	w.open("aws =")
	w.attr("source", `"hashicorp/aws"`)
	w.close()
	w.close()
	w.close()
	w.line("")
	w.open(`provider "aws"`)
	w.attr("region", "var.region")
	w.close()
	g.variables()
	g.network()
	for _, n := range g.t.nodes {
		g.node(n)
	}
	return []byte(strings.TrimRight(w.String(), "\n") + "\n")
}

// plan names every resource before anything is written so references work in
// any order: VPCs, subnets, then nodes, each in ID order.
func (g *tfGen) plan() {
	for _, b := range g.t.boxes {
		if b.Kind != archdoc.BoxVPC {
			continue
		}
		g.vpcIdx[b.ID] = len(g.vpcIdx)
		g.name[b.ID] = tfIdent(b.Label, "vpc", g.used)
	}
	for _, b := range g.t.boxes {
		if b.Kind != archdoc.BoxAZ {
			continue
		}
		if v := g.enclosing(b.ID, archdoc.BoxVPC); v != "" {
			g.azVPC[b.ID] = v
			g.subnets[v] = append(g.subnets[v], b.ID)
			g.name[b.ID] = tfIdent(b.Label, "subnet", g.used)
		}
	}
	for _, n := range g.t.nodes {
		if isAWSNode(n) {
			fallback := "node"
			if knownKind(n.Kind) {
				fallback = string(n.Kind)
			}
			g.name[n.ID] = tfIdent(n.Label, fallback, g.used)
		}
	}
}

// enclosing is the nearest box of kind k around box id, id included ("" = none).
func (g *tfGen) enclosing(id string, k archdoc.BoxKind) string {
	path := g.t.boxPath(id)
	for i := len(path) - 1; i >= 0; i-- {
		if g.t.box[path[i]].Kind == k {
			return path[i]
		}
	}
	return ""
}

func (g *tfGen) kindNodes(ok func(*archdoc.Node) bool) bool {
	return slices.ContainsFunc(g.t.nodes, func(n *archdoc.Node) bool { return isAWSNode(n) && ok(n) })
}

func isCompute(n *archdoc.Node) bool {
	return n.Kind == archdoc.KindAPI || n.Kind == archdoc.KindWorker
}

func (g *tfGen) variables() {
	w := &g.w
	w.line("")
	w.open(`variable "region"`)
	w.attr("type", "string")
	for _, b := range g.t.boxes {
		if _, ok := pricing.RegionOf(pricing.AWS, b.RegionCode); ok && b.Kind == archdoc.BoxRegion {
			w.attr("default", hclString(b.RegionCode))
			break
		}
	}
	w.close()
	if g.kindNodes(isCompute) {
		w.line("")
		w.open(`variable "ami_id"`)
		w.attr("type", "string")
		w.close()
	}
	if g.kindNodes(func(n *archdoc.Node) bool { return n.Kind == archdoc.KindDBPrimary }) {
		w.line("")
		w.open(`variable "db_username"`)
		w.attr("type", "string")
		w.close()
	}
	for _, n := range g.t.nodes {
		if isAWSNode(n) && n.Kind == archdoc.KindCDN && g.cdnOrigin(n) == "" {
			w.line("")
			w.open(`variable "` + g.name[n.ID] + `_origin_domain"`)
			w.attr("type", "string")
			w.close()
		}
	}
}

func (g *tfGen) tags(label string) {
	w := &g.w
	w.line("")
	w.open("tags =")
	w.attr("Name", hclString(label))
	w.close()
}

func (g *tfGen) network() {
	w := &g.w
	if len(g.azVPC) > 0 {
		w.line("")
		w.open(`data "aws_availability_zones" "available"`)
		w.attr("state", `"available"`)
		w.close()
	}
	for _, b := range g.t.boxes {
		if b.Kind != archdoc.BoxVPC {
			continue
		}
		name := g.name[b.ID]
		w.line("")
		w.open(`resource "aws_vpc" "` + name + `"`)
		w.attr("cidr_block", `"10.`+strconv.Itoa(g.vpcIdx[b.ID]%256)+`.0.0/16"`)
		g.tags(boxLabel(b))
		w.close()
		w.line("")
		w.open(`resource "aws_security_group" "` + name + `"`)
		w.attr("vpc_id", "aws_vpc."+name+".id")
		w.line("")
		w.comment("TODO: ingress and egress rules")
		g.tags(boxLabel(b))
		w.close()
		for i, az := range g.subnets[b.ID] {
			w.line("")
			w.open(`resource "aws_subnet" "` + g.name[az] + `"`)
			w.attr("vpc_id", "aws_vpc."+name+".id")
			w.attr("cidr_block", `"10.`+strconv.Itoa(g.vpcIdx[b.ID]%256)+"."+strconv.Itoa(i%256)+`.0/24"`)
			w.attr("availability_zone", "data.aws_availability_zones.available.names["+strconv.Itoa(i)+"]")
			g.tags(boxLabel(g.t.box[az]))
			w.close()
		}
	}
	for _, b := range g.t.boxes {
		if b.Kind == archdoc.BoxAZ && g.azVPC[b.ID] == "" {
			w.line("")
			w.comment("TODO: AZ " + hclString(boxLabel(b)) + " is not inside a VPC; no subnet generated")
		}
	}
}

// placement is the node's VPC resource name and subnet references: its AZ's
// subnet, or every subnet of its VPC.
func (g *tfGen) placement(n *archdoc.Node) (vpc string, subnets []string) {
	box := g.t.nodeBox(n)
	v := g.enclosing(box, archdoc.BoxVPC)
	if v == "" {
		return "", nil
	}
	if az := g.enclosing(box, archdoc.BoxAZ); az != "" && g.azVPC[az] == v {
		return g.name[v], []string{"aws_subnet." + g.name[az] + ".id"}
	}
	for _, az := range g.subnets[v] {
		subnets = append(subnets, "aws_subnet."+g.name[az]+".id")
	}
	return g.name[v], subnets
}

// lbsInFront lists the AWS load balancers with an edge to n.
func (g *tfGen) lbsInFront(n *archdoc.Node) []string {
	var out []string
	for _, e := range g.t.edges {
		if p := g.t.node[e.From]; e.To == n.ID && p.Kind == archdoc.KindLB && isAWSNode(p) {
			out = append(out, "aws_lb_target_group."+g.name[p.ID]+".arn")
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// cdnOrigin is the DNS name reference of the first AWS LB the CDN sends to.
func (g *tfGen) cdnOrigin(n *archdoc.Node) string {
	for _, e := range g.t.edges {
		if t := g.t.node[e.To]; e.From == n.ID && t.Kind == archdoc.KindLB && isAWSNode(t) {
			return "aws_lb." + g.name[t.ID] + ".dns_name"
		}
	}
	return ""
}

// replicaSource is the primary a replica copies: one that fails over to it,
// else the first AWS primary.
func (g *tfGen) replicaSource(n *archdoc.Node) string {
	first := ""
	for _, p := range g.t.nodes {
		if p.Kind != archdoc.KindDBPrimary || !isAWSNode(p) {
			continue
		}
		if p.Params.FailoverTo == n.ID {
			return g.name[p.ID]
		}
		if first == "" {
			first = g.name[p.ID]
		}
	}
	return first
}

// dashed is a resource name as an AWS identifier ("a-b", max bytes).
func dashed(name string, maxLen int) string {
	s := strings.ReplaceAll(name, "_", "-")
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return strings.TrimRight(s, "-")
}

func (g *tfGen) node(n *archdoc.Node) {
	w := &g.w
	switch n.Kind {
	case archdoc.KindClient, archdoc.KindExternal:
		return
	}
	kind := "node"
	if knownKind(n.Kind) {
		kind = string(n.Kind)
	}
	label := nodeLabel(n)
	if !isAWSNode(n) {
		w.line("")
		w.comment("TODO: non-AWS provider (" + kind + " " + hclString(label) + ")")
		return
	}
	name := g.name[n.ID]
	vpc, subnets := g.placement(n)
	instType := hclString(n.InstanceType)
	inst := max(n.Instances, 0)
	res := func(typ string) {
		w.line("")
		w.open(`resource "` + typ + `" "` + name + `"`)
	}
	switch n.Kind {
	case archdoc.KindLB:
		res("aws_lb")
		w.attr("load_balancer_type", `"application"`)
		w.attr("subnets", list(subnets))
		if vpc != "" {
			w.attr("security_groups", list([]string{"aws_security_group." + vpc + ".id"}))
		} else {
			w.line("")
			w.comment("TODO: place the load balancer in a VPC")
		}
		g.tags(label)
		w.close()
		res("aws_lb_target_group")
		w.attr("port", "80")
		w.attr("protocol", `"HTTP"`)
		if vpc != "" {
			w.attr("vpc_id", "aws_vpc."+vpc+".id")
		}
		g.tags(label)
		w.close()
		res("aws_lb_listener")
		w.attr("load_balancer_arn", "aws_lb."+name+".arn")
		w.attr("port", "80")
		w.attr("protocol", `"HTTP"`)
		w.line("")
		w.open("default_action")
		w.attr("type", `"forward"`)
		w.attr("target_group_arn", "aws_lb_target_group."+name+".arn")
		w.close()
		w.close()
	case archdoc.KindAPI, archdoc.KindWorker:
		res("aws_launch_template")
		w.attr("image_id", "var.ami_id")
		w.attr("instance_type", instType)
		g.tags(label)
		w.close()
		lo, hi, want := inst, inst, inst
		if a := n.Autoscale; a.Enabled {
			lo = max(a.Min, 0)
			hi = max(a.Max, lo)
			want = min(max(inst, lo), hi)
		}
		res("aws_autoscaling_group")
		w.attr("min_size", strconv.Itoa(lo))
		w.attr("max_size", strconv.Itoa(hi))
		w.attr("desired_capacity", strconv.Itoa(want))
		w.attr("vpc_zone_identifier", list(subnets))
		if tgs := g.lbsInFront(n); len(tgs) > 0 {
			w.attr("target_group_arns", list(tgs))
		}
		if len(subnets) == 0 {
			w.line("")
			w.comment("TODO: place the group in VPC subnets")
		}
		w.line("")
		w.open("launch_template")
		w.attr("id", "aws_launch_template."+name+".id")
		w.attr("version", `"$Latest"`)
		w.close()
		w.line("")
		w.open("tag")
		w.attr("key", `"Name"`)
		w.attr("value", hclString(label))
		w.attr("propagate_at_launch", "true")
		w.close()
		w.close()
	case archdoc.KindCache:
		res("aws_elasticache_cluster")
		w.attr("cluster_id", `"`+dashed(name, 40)+`"`)
		w.attr("engine", `"redis"`)
		w.attr("node_type", instType)
		w.attr("num_cache_nodes", "1")
		g.tags(label)
		w.close()
	case archdoc.KindDBPrimary:
		res("aws_db_instance")
		w.attr("identifier", `"`+dashed(name, 63)+`"`)
		w.attr("engine", `"postgres"`)
		w.attr("instance_class", instType)
		w.attr("allocated_storage", strconv.Itoa(max(n.Disk.SizeGB, 20)))
		w.attr("username", "var.db_username")
		w.attr("manage_master_user_password", "true")
		w.attr("multi_az", strconv.FormatBool(inst > 1))
		g.tags(label)
		w.close()
	case archdoc.KindDBReplica:
		res("aws_db_instance")
		w.attr("identifier", `"`+dashed(name, 63)+`"`)
		if src := g.replicaSource(n); src != "" {
			w.attr("replicate_source_db", "aws_db_instance."+src+".identifier")
		}
		w.attr("instance_class", instType)
		if g.replicaSource(n) == "" {
			w.line("")
			w.comment("TODO: no primary database to replicate from")
		}
		g.tags(label)
		w.close()
	case archdoc.KindQueue:
		res("aws_sqs_queue")
		w.attr("name", `"`+dashed(name, 80)+`"`)
		g.tags(label)
		w.close()
	case archdoc.KindObjectStorage:
		res("aws_s3_bucket")
		w.attr("bucket_prefix", `"`+dashed(name, 36)+`-"`)
		g.tags(label)
		w.close()
	case archdoc.KindSearch:
		res("aws_opensearch_domain")
		w.attr("domain_name", `"`+dashed(name, 28)+`"`)
		w.line("")
		w.open("cluster_config")
		w.attr("instance_type", instType)
		w.attr("instance_count", strconv.Itoa(max(inst, 1)))
		w.close()
		g.tags(label)
		w.close()
	case archdoc.KindCDN:
		g.cdn(n, name, label)
	default:
		w.line("")
		w.comment("TODO: unknown component (" + hclString(label) + ")")
	}
}

func (g *tfGen) cdn(n *archdoc.Node, name, label string) {
	w := &g.w
	origin := g.cdnOrigin(n)
	if origin == "" {
		origin = "var." + name + "_origin_domain"
	}
	w.line("")
	w.open(`resource "aws_cloudfront_distribution" "` + name + `"`)
	w.attr("enabled", "true")
	w.line("")
	w.open("origin")
	w.attr("domain_name", origin)
	w.attr("origin_id", `"`+dashed(name, 64)+`"`)
	w.line("")
	w.open("custom_origin_config")
	w.attr("http_port", "80")
	w.attr("https_port", "443")
	w.attr("origin_protocol_policy", `"https-only"`)
	w.attr("origin_ssl_protocols", `["TLSv1.2"]`)
	w.close()
	w.close()
	w.line("")
	w.open("default_cache_behavior")
	w.attr("target_origin_id", `"`+dashed(name, 64)+`"`)
	w.attr("viewer_protocol_policy", `"redirect-to-https"`)
	w.attr("allowed_methods", `["GET", "HEAD"]`)
	w.attr("cached_methods", `["GET", "HEAD"]`)
	w.line("")
	w.open("forwarded_values")
	w.attr("query_string", "false")
	w.line("")
	w.open("cookies")
	w.attr("forward", `"none"`)
	w.close()
	w.close()
	w.close()
	w.line("")
	w.open("restrictions")
	w.open("geo_restriction")
	w.attr("restriction_type", `"none"`)
	w.close()
	w.close()
	w.line("")
	w.open("viewer_certificate")
	w.attr("cloudfront_default_certificate", "true")
	w.close()
	g.tags(label)
	w.close()
}
