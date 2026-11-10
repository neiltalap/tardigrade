select
	min(a,b),
	min(a order by b),
	min(a order by b, c),
	min(a order by b, c, d),
	min(distinct a,b),
	min(),
	min() filter (where x >3 and y < 4)
