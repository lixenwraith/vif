package genetic

// Codec translates between genotype (evolvable) and phenotype (usable) representations
type Codec[G Solution, P any] interface {
	Encode(P) G
	Decode(G) P
	Clamp(G) G
}